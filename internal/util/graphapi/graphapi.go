// Package graphapi provides a GraphQL schema generator using reflection.
package graphapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/commentparse"
	"github.com/ziyan/teanode/internal/util/deferutil"
)

var log = logging.MustGetLogger("graphapi")

type contextKey int

const (
	resolveParametersKey contextKey = iota
)

func contextWithResolveParameters(ctx context.Context, resolveParameters graphql.ResolveParams) context.Context {
	return context.WithValue(ctx, resolveParametersKey, resolveParameters)
}

func contextResolveParameters(ctx context.Context) graphql.ResolveParams {
	return ctx.Value(resolveParametersKey).(graphql.ResolveParams)
}

// Selected checks if a field is selected to be returned.
func Selected(ctx context.Context, selectionPath ...string) bool {
	resolveParameters := contextResolveParameters(ctx)
	var selectedFields []*ast.Field
	for _, selectedField := range resolveParameters.Info.FieldASTs {
		if selectedField.Name == nil || selectedField.Name.Value != resolveParameters.Info.FieldName {
			continue
		}
		// gather fields selected for the next level
		selectedFields = selectedFields[:0]
		if selectedField.SelectionSet != nil {
			for _, selection := range selectedField.SelectionSet.Selections {
				selectedFields = append(selectedFields, selection.(*ast.Field))
			}
		}
		break
	}
	for _, selectedFieldName := range selectionPath {
		found := false
		for _, selectedField := range selectedFields {
			if selectedField.Name == nil || selectedField.Name.Value != selectedFieldName {
				continue
			}
			found = true
			// gather fields selected for the next level
			selectedFields = selectedFields[:0]
			if selectedField.SelectionSet != nil {
				for _, selection := range selectedField.SelectionSet.Selections {
					selectedFields = append(selectedFields, selection.(*ast.Field))
				}
			}
			break
		}
		if !found {
			// if not found at a level, then no need to check the rest
			return false
		}
	}
	return true
}

var (
	errorType     = reflect.TypeOf((*error)(nil)).Elem()
	timeType      = reflect.TypeOf(time.Time{})
	contextType   = reflect.TypeOf((*context.Context)(nil)).Elem()
	byteSliceType = reflect.TypeOf([]byte{})
)

var Void = graphql.NewScalar(graphql.ScalarConfig{
	Name: "Void",
	Serialize: func(interface{}) interface{} {
		return nil
	},
})

var Data = graphql.NewScalar(graphql.ScalarConfig{
	Name: "Data",
	Serialize: func(value interface{}) interface{} {
		return base64.StdEncoding.EncodeToString(value.([]byte))
	},
	ParseValue: func(value interface{}) interface{} {
		raw, err := base64.StdEncoding.DecodeString(value.(string))
		if err != nil {
			log.Errorf("graphapi: failed to decode base64: %w", err)
			return nil
		}
		return raw
	},
	ParseLiteral: func(value ast.Value) interface{} {
		return nil
	},
})

var Any = graphql.NewScalar(graphql.ScalarConfig{
	Name: "Any",
	Serialize: func(value interface{}) interface{} {
		return value
	},
	ParseValue: func(value interface{}) interface{} {
		return value
	},
	ParseLiteral: func(value ast.Value) interface{} {
		return value.GetValue()
	},
})

type GraphAPI interface {
	// Return the generated graphql schema
	Build() (graphql.Schema, error)

	Register(query, mutation, subscription interface{}) error
}

type graphApi struct {
	rootQuery        *graphql.Object
	rootMutation     *graphql.Object
	rootSubscription *graphql.Object
	inputTypes       map[reflect.Type]*graphql.InputObject
	outputTypes      map[reflect.Type]*graphql.Object
}

func New() GraphAPI {
	return &graphApi{
		rootQuery: graphql.NewObject(graphql.ObjectConfig{
			Name:   "RootQuery",
			Fields: graphql.Fields{},
		}),
		rootMutation: graphql.NewObject(graphql.ObjectConfig{
			Name:   "RootMutation",
			Fields: graphql.Fields{},
		}),
		rootSubscription: graphql.NewObject(graphql.ObjectConfig{
			Name:   "RootSubscription",
			Fields: graphql.Fields{},
		}),
		inputTypes:  make(map[reflect.Type]*graphql.InputObject),
		outputTypes: make(map[reflect.Type]*graphql.Object),
	}
}

func (self *graphApi) Build() (graphql.Schema, error) {
	config := graphql.SchemaConfig{
		Query:    self.rootQuery,
		Mutation: self.rootMutation,
	}
	// A root type with no fields is not a valid schema, so a caller that
	// registers no subscriptions gets a schema without the root rather than an
	// error about an empty type.
	if len(self.rootSubscription.Fields()) > 0 {
		config.Subscription = self.rootSubscription
	}
	return graphql.NewSchema(config)
}

func (self *graphApi) register(root *graphql.Object, queryMutationSubscription interface{}, isSubscription bool) error {
	if queryMutationSubscription == nil {
		return nil
	}
	interfaceValue := reflect.ValueOf(queryMutationSubscription).Elem()
	interfaceType := reflect.TypeOf(queryMutationSubscription).Elem()
	for i := 0; i < interfaceType.NumMethod(); i++ {
		method := interfaceType.Method(i)
		methodValue := interfaceValue.MethodByName(method.Name)
		field, err := self.buildMethodField(interfaceType, method, methodValue, isSubscription)
		if err != nil {
			return err
		}
		root.AddFieldConfig(method.Name, field)
	}
	return nil
}

func (self *graphApi) Register(query, mutation, subscription interface{}) error {
	if err := self.register(self.rootQuery, query, false); err != nil {
		return err
	}
	if err := self.register(self.rootMutation, mutation, false); err != nil {
		return err
	}
	if err := self.register(self.rootSubscription, subscription, true); err != nil {
		return err
	}
	return nil
}

func validateMethodSignature(method reflect.Method) error {
	methodType := method.Type
	if methodType.NumIn() < 1 || methodType.NumIn() > 2 {
		return fmt.Errorf("graphapi: method %q has %d arguments", method.Name, methodType.NumIn())
	}
	if methodType.In(0) != contextType {
		return fmt.Errorf("graphapi: method %q does not take context as first argument", method.Name)
	}
	if methodType.NumOut() < 1 || methodType.NumOut() > 2 {
		return fmt.Errorf("graphapi: method %q has %d return values", method.Name, methodType.NumOut())
	}
	if !methodType.Out(methodType.NumOut() - 1).Implements(errorType) {
		return fmt.Errorf("graphapi: method %q has %v as last return value, but expecting error", method.Name, methodType.Out(methodType.NumOut()-1))
	}
	return nil
}

func (self *graphApi) buildMethodArguments(method reflect.Method) (graphql.FieldConfigArgument, reflect.Type, error) {
	methodType := method.Type
	if methodType.NumIn() != 2 {
		return nil, nil, nil
	}
	argumentType := methodType.In(1)
	arguments := make(graphql.FieldConfigArgument)
	for i := 0; i < argumentType.NumField(); i++ {
		field := argumentType.Field(i)
		fieldName := strings.SplitN(field.Tag.Get("graphql"), ",", 2)[0]
		if fieldName == "" {
			fieldName = strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		}
		if fieldName == "" || fieldName == "-" {
			return nil, nil, fmt.Errorf("graphapi: method %q argument field %q has no graphql or json tag", method.Name, field.Name)
		}
		description := commentparse.GetStructFieldComment(argumentType.PkgPath(), argumentType.Name(), field.Name)
		if description == "" {
			fieldType := field.Type
			// Dereference the type if it's a pointer to a type, so we can get the real type name
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			description = commentparse.GetStructComment(fieldType.PkgPath(), fieldType.Name())
		}
		arguments[fieldName] = &graphql.ArgumentConfig{
			Description: description,
			// An argument is required unless it says otherwise, which is the
			// right default; a list that the caller may simply omit says so
			// with the same tag an input field uses.
			Type: self.translateInputType(field.Type, field.Tag.Get("graphapi") == "nullable"),
		}
	}
	return arguments, argumentType, nil
}

func (self *graphApi) buildMethodReturn(method reflect.Method, isSubscription bool) (graphql.Type, error) {
	var returnType graphql.Type
	methodType := method.Type
	if isSubscription {
		if methodType.NumOut() != 2 {
			return nil, fmt.Errorf("graphapi: subscription %q should have two return values", method.Name)
		}
		methodReturnType := methodType.Out(0)
		if methodReturnType.Kind() != reflect.Chan {
			return nil, fmt.Errorf("graphapi: subscription %q should return a channel", method.Name)
		}
		returnType = self.translateOutputType(methodReturnType.Elem())
	} else {
		if methodType.NumOut() == 2 {
			returnType = self.translateOutputType(methodType.Out(0))
		} else {
			returnType = Void
		}
	}
	return returnType, nil
}

func (self *graphApi) callMethod(methodValue reflect.Value, argumentType reflect.Type, resolveParameters graphql.ResolveParams) (resultValue reflect.Value, err error) {
	defer func() {
		if message := recover(); message != nil {
			log.Errorf("panic: %s\n", message, string(debug.Stack()))
			err = fmt.Errorf("graphapi: panic: %s", message)
		}
	}()
	ctx := contextWithResolveParameters(resolveParameters.Context, resolveParameters)
	// build arguments to call the method
	argumentValues := make([]reflect.Value, 0, 2)
	argumentValues = append(argumentValues, reflect.ValueOf(ctx))
	if argumentType != nil {
		argumentValue := reflect.New(argumentType).Elem()
		for i := 0; i < argumentType.NumField(); i++ {
			field := argumentType.Field(i)
			fieldName := strings.SplitN(field.Tag.Get("graphql"), ",", 2)[0]
			if fieldName == "" {
				fieldName = strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			}
			fieldValue := argumentValue.Field(i)
			fieldValue.Set(self.coerceInputValue(resolveParameters.Args[fieldName], field.Type))
		}
		argumentValues = append(argumentValues, argumentValue)
	}
	// call method
	returnValues := methodValue.Call(argumentValues)
	if !returnValues[len(returnValues)-1].IsNil() {
		err = returnValues[len(returnValues)-1].Interface().(error)
	}
	if len(returnValues) > 1 {
		resultValue = returnValues[0]
	}
	return resultValue, err
}

func (self *graphApi) buildMethodField(interfaceType reflect.Type, method reflect.Method, methodValue reflect.Value, isSubscription bool) (*graphql.Field, error) {
	if err := validateMethodSignature(method); err != nil {
		return nil, err
	}
	arguments, argumentType, err := self.buildMethodArguments(method)
	if err != nil {
		return nil, err
	}
	returnType, err := self.buildMethodReturn(method, isSubscription)
	if err != nil {
		return nil, err
	}
	field := &graphql.Field{
		Description: commentparse.GetInterfaceMethodComment(interfaceType.PkgPath(), interfaceType.Name(), method.Name),
		Type:        returnType,
		Args:        arguments,
	}
	if !isSubscription {
		field.Resolve = func(resolveParameters graphql.ResolveParams) (interface{}, error) {
			resultValue, err := self.callMethod(methodValue, argumentType, resolveParameters)
			if !resultValue.IsValid() || !resultValue.CanInterface() {
				return nil, err
			}
			return resultValue.Interface(), err
		}
	} else {
		field.Resolve = func(resolveParameters graphql.ResolveParams) (interface{}, error) {
			return resolveParameters.Source, nil
		}
		field.Subscribe = func(resolveParameters graphql.ResolveParams) (interface{}, error) {
			channelValue, err := self.callMethod(methodValue, argumentType, resolveParameters)
			if err != nil {
				return nil, err
			}
			convertedChannel := make(chan interface{})
			go func() {
				defer deferutil.Recover()
				defer close(convertedChannel)
				for {
					resultValue, ok := channelValue.Recv()
					if !ok {
						return
					}
					convertedChannel <- resultValue.Interface()
				}
			}()
			return convertedChannel, nil
		}
	}
	return field, nil
}

func (self *graphApi) translateOutputType(modelType reflect.Type) graphql.Type {
	nullable := false
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
		nullable = true
	}
	if modelType.Kind() == reflect.Interface {
		nullable = true
	}
	if modelType == byteSliceType {
		return makeNullable(Data, nullable)
	}
	if modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array {
		return makeNullable(graphql.NewList(self.translateOutputType(modelType.Elem())), nullable)
	}
	outputType := translateCommonType(modelType)
	if outputType != nil {
		return makeNullable(outputType, nullable)
	}
	if modelType.Kind() == reflect.Struct {
		if outputType, ok := self.outputTypes[modelType]; ok {
			return makeNullable(outputType, nullable)
		}
		outputType := graphql.NewObject(graphql.ObjectConfig{
			Name:        modelType.Name(),
			Fields:      graphql.Fields{},
			Description: commentparse.GetStructComment(modelType.PkgPath(), modelType.Name()),
		})
		self.outputTypes[modelType] = outputType
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			if field.Anonymous {
				continue
			}
			fieldName := strings.SplitN(field.Tag.Get("graphql"), ",", 2)[0]
			if fieldName == "" {
				fieldName = strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			}
			if fieldName == "" || fieldName == "-" {
				continue
			}
			if field.Tag.Get("graphapi") == "inputonly" || field.Tag.Get("graphapi") == "ignore" {
				continue
			}
			description := commentparse.GetStructFieldComment(modelType.PkgPath(), modelType.Name(), field.Name)
			if description == "" {
				fieldType := field.Type
				// Dereference the type if it's a pointer to a type, so we can get the real type name
				for fieldType.Kind() == reflect.Pointer {
					fieldType = fieldType.Elem()
				}
				description = commentparse.GetStructComment(fieldType.PkgPath(), fieldType.Name())
			}
			outputType.AddFieldConfig(fieldName, &graphql.Field{
				Description: description,
				Type:        self.translateOutputType(field.Type),
			})
		}
		return makeNullable(outputType, nullable)
	}
	return makeNullable(Any, nullable)
}

func (self *graphApi) translateInputType(modelType reflect.Type, defaultNullable bool) graphql.Type {
	nullable := defaultNullable
	for modelType.Kind() == reflect.Pointer {
		modelType = modelType.Elem()
		nullable = true
	}
	if modelType.Kind() == reflect.Interface {
		nullable = true
	}
	if modelType == byteSliceType {
		return makeNullable(Data, nullable)
	}
	if modelType.Kind() == reflect.Slice || modelType.Kind() == reflect.Array {
		return makeNullable(graphql.NewList(self.translateInputType(modelType.Elem(), false)), nullable)
	}
	inputType := translateCommonType(modelType)
	if inputType != nil {
		return makeNullable(inputType, nullable)
	}
	if modelType.Kind() == reflect.Struct {
		if inputType, ok := self.inputTypes[modelType]; ok {
			return makeNullable(inputType, nullable)
		}
		inputType := graphql.NewInputObject(graphql.InputObjectConfig{
			Name:        fmt.Sprintf("%sInput", modelType.Name()),
			Fields:      graphql.InputObjectConfigFieldMap{},
			Description: commentparse.GetStructComment(modelType.PkgPath(), modelType.Name()),
		})
		self.inputTypes[modelType] = inputType
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			if field.Anonymous {
				continue
			}
			fieldName := strings.SplitN(field.Tag.Get("graphql"), ",", 2)[0]
			if fieldName == "" {
				fieldName = strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			}
			if fieldName == "" || fieldName == "-" {
				continue
			}
			if field.Tag.Get("graphapi") == "outputonly" || field.Tag.Get("graphapi") == "ignore" {
				continue
			}
			inputNullable := field.Tag.Get("graphapi") == "nullable"
			switch fieldName {
			case "id", "deleted", "extendable":
				inputNullable = true
			}
			description := commentparse.GetStructFieldComment(modelType.PkgPath(), modelType.Name(), field.Name)
			if description == "" {
				fieldType := field.Type
				// Dereference the type if it's a pointer to a type, so we can get the real type name
				for fieldType.Kind() == reflect.Pointer {
					fieldType = fieldType.Elem()
				}
				description = commentparse.GetStructComment(fieldType.PkgPath(), fieldType.Name())
			}
			inputType.AddFieldConfig(fieldName, &graphql.InputObjectFieldConfig{
				Description: description,
				Type:        self.translateInputType(field.Type, inputNullable),
			})
		}
		return makeNullable(inputType, nullable)
	}
	return makeNullable(Any, nullable)
}

func (self *graphApi) coerceInputValue(raw interface{}, modelType reflect.Type) reflect.Value {
	if raw == nil {
		return reflect.Zero(modelType)
	}
	if modelType.Kind() == reflect.Pointer {
		value := reflect.New(modelType.Elem())
		value.Elem().Set(self.coerceInputValue(raw, modelType.Elem()))
		return value
	}
	if modelType == byteSliceType {
		return reflect.ValueOf(raw)
	}
	if modelType.Kind() == reflect.Slice {
		rawValue := reflect.ValueOf(raw)
		value := reflect.MakeSlice(modelType, 0, rawValue.Len())
		for i := 0; i < rawValue.Len(); i++ {
			value = reflect.Append(value, self.coerceInputValue(rawValue.Index(i).Interface(), modelType.Elem()))
		}
		return value
	}
	if modelType.Kind() == reflect.Array {
		rawValue := reflect.ValueOf(raw)
		value := reflect.New(modelType).Elem()
		for i := 0; i < rawValue.Len(); i++ {
			value.Index(i).Set(self.coerceInputValue(rawValue.Index(i).Interface(), modelType.Elem()))
		}
		return value
	}
	if value := coerceCommonType(raw, modelType); value.IsValid() {
		return value
	}

	if _, ok := self.inputTypes[modelType]; ok {
		rawMap := raw.(map[string]interface{})
		valuePointer := reflect.New(modelType)
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			value := valuePointer.Elem().Field(i)
			if field.Anonymous {
				continue
			}
			fieldName := strings.SplitN(field.Tag.Get("graphql"), ",", 2)[0]
			if fieldName == "" {
				fieldName = strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
			}
			if fieldName == "" || fieldName == "-" {
				continue
			}
			rawInterface, ok := rawMap[fieldName]
			if !ok {
				continue
			}
			value.Set(self.coerceInputValue(rawInterface, field.Type))
		}
		return valuePointer.Elem()
	}
	return reflect.ValueOf(raw)
}

func translateCommonType(modelType reflect.Type) graphql.Type {
	switch modelType.Kind() {
	case reflect.Bool:
		return graphql.Boolean
	case reflect.String:
		return graphql.String
	case reflect.Int, reflect.Uint, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Uint64, reflect.Uint32, reflect.Uint16:
		return graphql.Int
	case reflect.Float64, reflect.Float32:
		return graphql.Float
	case reflect.Struct:
		if modelType == timeType {
			return graphql.DateTime
		}
	}
	return nil
}

// coerceCommonType converts a decoded JSON value into the model's type.
//
// The result is converted to the model's exact type before it is returned,
// not just to something of the same kind. A named type over a builtin —
// "type Operation string", which is what every enum in an input is — matches
// on kind but is not assignable from a plain string, and assigning one to the
// other panics inside reflect rather than returning an error.
func coerceCommonType(value interface{}, modelType reflect.Type) reflect.Value {
	coerced := coerceCommonKind(value, modelType)
	if coerced.IsValid() && coerced.Type() != modelType && coerced.Type().ConvertibleTo(modelType) {
		return coerced.Convert(modelType)
	}
	return coerced
}

func coerceCommonKind(value interface{}, modelType reflect.Type) reflect.Value {
	switch modelType.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(value.(bool))
	case reflect.String:
		return reflect.ValueOf(value.(string))
	case reflect.Int:
		return reflect.ValueOf(value.(int))
	case reflect.Uint:
		return reflect.ValueOf(uint(value.(int)))
	case reflect.Int64:
		return reflect.ValueOf(int64(value.(int)))
	case reflect.Int32:
		return reflect.ValueOf(int32(value.(int)))
	case reflect.Int16:
		return reflect.ValueOf(int16(value.(int)))
	case reflect.Uint64:
		return reflect.ValueOf(uint64(value.(int)))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(value.(int)))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(value.(int)))
	case reflect.Float64:
		return reflect.ValueOf(value.(float64))
	case reflect.Float32:
		return reflect.ValueOf(float32(value.(float64)))
	case reflect.Struct:
		if modelType == timeType {
			return reflect.ValueOf(value.(time.Time))
		}
	}
	return reflect.Value{}
}

func makeNullable(graphType graphql.Type, nullable bool) graphql.Type {
	if nullable {
		return graphType
	}
	return graphql.NewNonNull(graphType)
}
