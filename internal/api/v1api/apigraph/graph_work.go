package apigraph

import (
	"fmt"
	"math"
	"strconv"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/config"
)

// checkGraphWork charges selected fields for the pages requested at their
// ancestors. It bounds pagination-driven fanout, not unpaginated collection
// sizes or resolver runtime. Individual resolvers still own their row limits.
func checkGraphWork(schema *graphql.Schema, document *ast.Document, operation *ast.OperationDefinition, suppliedVariables map[string]interface{}, limits config.GraphQL) error {
	variables := make(map[string]interface{}, len(suppliedVariables))
	for variableName, variableValue := range suppliedVariables {
		variables[variableName] = variableValue
	}
	for _, definition := range operation.VariableDefinitions {
		variableName := definition.Variable.Name.Value
		if _, isSupplied := variables[variableName]; !isSupplied && definition.DefaultValue != nil {
			variables[variableName] = graphArgumentValue(definition.DefaultValue, variables)
		}
	}
	fragments := make(map[string]*ast.FragmentDefinition)
	for _, definition := range document.Definitions {
		if fragment, isFragment := definition.(*ast.FragmentDefinition); isFragment {
			fragments[fragment.Name.Value] = fragment
		}
	}
	workCount := 0
	var walkSelections func(*ast.SelectionSet, graphql.Type, int) error
	walkSelections = func(selections *ast.SelectionSet, parentType graphql.Type, multiplier int) error {
		if selections == nil {
			return nil
		}
		for _, selection := range selections.Selections {
			switch selected := selection.(type) {
			case *ast.Field:
				field := graphFieldDefinition(parentType, selected.Name.Value)
				pageSize, err := graphPageSize(field, selected, variables, limits.MaximumListItemCount)
				if err != nil {
					return err
				}
				if multiplier > limits.MaximumWorkCount/pageSize {
					return fmt.Errorf("GraphQL operation exceeds maximum work count")
				}
				fieldWorkCount := multiplier * pageSize
				if fieldWorkCount > limits.MaximumWorkCount-workCount {
					return fmt.Errorf("GraphQL operation exceeds maximum work count")
				}
				workCount += fieldWorkCount
				var childType graphql.Type
				if field != nil {
					childType = field.Type
				}
				if err := walkSelections(selected.SelectionSet, childType, fieldWorkCount); err != nil {
					return err
				}
			case *ast.InlineFragment:
				fragmentType := parentType
				if selected.TypeCondition != nil {
					fragmentType = schema.Type(selected.TypeCondition.Name.Value)
				}
				if err := walkSelections(selected.SelectionSet, fragmentType, multiplier); err != nil {
					return err
				}
			case *ast.FragmentSpread:
				fragment := fragments[selected.Name.Value]
				if fragment != nil {
					if err := walkSelections(fragment.SelectionSet, schema.Type(fragment.TypeCondition.Name.Value), multiplier); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	var rootType graphql.Type = schema.QueryType()
	switch operation.Operation {
	case "mutation":
		rootType = schema.MutationType()
	case "subscription":
		rootType = schema.SubscriptionType()
	}
	return walkSelections(operation.SelectionSet, rootType, 1)
}

func graphFieldDefinition(parentType graphql.Type, fieldName string) *graphql.FieldDefinition {
	if parentType == nil {
		return nil
	}
	switch named := graphql.GetNamed(parentType).(type) {
	case *graphql.Object:
		return named.Fields()[fieldName]
	case *graphql.Interface:
		return named.Fields()[fieldName]
	}
	return nil
}

func graphPageSize(field *graphql.FieldDefinition, selected *ast.Field, variables map[string]interface{}, maximumListItemCount int) (int, error) {
	if field == nil {
		return 1, nil
	}
	for _, definition := range field.Args {
		if definition.Name() != "first" && definition.Name() != "pagination" {
			continue
		}
		argumentValue := definition.DefaultValue
		for _, argument := range selected.Arguments {
			if argument.Name.Value == definition.Name() {
				argumentValue = graphArgumentValue(argument.Value, variables)
				break
			}
		}
		if definition.Name() == "pagination" {
			pagination, _ := argumentValue.(map[string]interface{})
			argumentValue = pagination["first"]
		}
		pageSize := int64(api.MaximumPageSize)
		switch requested := argumentValue.(type) {
		case float64:
			if math.IsNaN(requested) || math.IsInf(requested, 0) || math.Trunc(requested) != requested {
				return 0, fmt.Errorf("GraphQL page size must be an integer")
			}
			if requested > math.MaxInt32 {
				return 0, fmt.Errorf("GraphQL page size exceeds maximum list item count")
			}
			if requested > 0 {
				pageSize = int64(requested)
			}
		case int64:
			pageSize = requested
		case int:
			pageSize = int64(requested)
		case nil:
		default:
			return 0, fmt.Errorf("GraphQL page size must be an integer")
		}
		// Nonpositive sizes select a resolver's default page, not an empty
		// result. Charge the shared upper bound instead of allowing zero cost.
		if pageSize <= 0 {
			pageSize = api.MaximumPageSize
		}
		// GraphQL Int is signed 32-bit, also the narrowest supported Go int.
		// Keep the conversion safe even if the caller bypassed config validation.
		if pageSize > math.MaxInt32 || pageSize > int64(maximumListItemCount) {
			return 0, fmt.Errorf("GraphQL page size exceeds maximum list item count")
		}
		return int(pageSize), nil
	}
	return 1, nil
}

func graphArgumentValue(argument ast.Value, variables map[string]interface{}) interface{} {
	switch supplied := argument.(type) {
	case *ast.Variable:
		return variables[supplied.Name.Value]
	case *ast.IntValue:
		integer, err := strconv.ParseInt(supplied.Value, 10, 64)
		if err != nil {
			return math.Inf(1)
		}
		return integer
	case *ast.ObjectValue:
		fields := make(map[string]interface{}, len(supplied.Fields))
		for _, field := range supplied.Fields {
			fields[field.Name.Value] = graphArgumentValue(field.Value, variables)
		}
		return fields
	}
	return nil
}
