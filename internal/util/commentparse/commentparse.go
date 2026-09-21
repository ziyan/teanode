// Package commentparse parses source code comments and creates a source code comment database.
package commentparse

//go:generate go run -mod=vendor commentparse_generate.go

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/op/go-logging"
	"golang.org/x/tools/go/packages"
)

var log = logging.MustGetLogger("commentparse") //nolint:unused

// MustWriteGeneratedCode outputs the generated code to `outputDirectory/commentparse_gen.go`
func MustWriteGeneratedCode(parsedComments *comments) {
	code := parsedComments.generateCode()
	// format the generated source code
	source, err := format.Source([]byte(code))
	if err != nil {
		panic(fmt.Errorf("commentparse: failed to format generated source code: %w", err))
	}
	// write to the file
	if err := os.WriteFile("commentparse_gen.go", source, 0o644); err != nil {
		panic(fmt.Errorf("commentparse: %w", err))
	}
}

func trimComment(commentGroup *ast.CommentGroup) string {
	// trim the trailing new line
	return strings.TrimRight(strings.TrimFunc(commentGroup.Text(), func(value rune) bool {
		// remove the whitespace in the comments except the new lines and tabs
		return unicode.IsSpace(value) && value != '\n' && value != '\t'
	}), "\n")
}

type structComments struct {
	typeName    string
	typeComment string
	fields      map[string]string
	methods     map[string]string
}

func (self *structComments) addField(fieldName string, comment string) *structComments {
	if comment == "" {
		return self
	}
	if self.fields == nil {
		self.fields = make(map[string]string)
	}
	self.fields[fieldName] = comment
	return self
}

func (self *structComments) addMethod(methodName string, comment string) *structComments {
	if comment == "" {
		return self
	}
	if self.methods == nil {
		self.methods = make(map[string]string)
	}
	self.methods[methodName] = comment
	return self
}

func (self *structComments) getField(fieldName string) string {
	if self == nil {
		return ""
	}
	return self.fields[fieldName]
}

func (self *structComments) getMethod(methodName string) string {
	if self == nil {
		return ""
	}
	return self.methods[methodName]
}

type interfaceComments struct {
	typeName    string
	typeComment string
	methods     map[string]string
}

func (self *interfaceComments) addMethod(methodName string, comment string) *interfaceComments {
	if comment == "" {
		return self
	}
	if self.methods == nil {
		self.methods = make(map[string]string)
	}
	self.methods[methodName] = comment
	return self
}

func (self *interfaceComments) getMethod(methodName string) string {
	if self == nil {
		return ""
	}
	return self.methods[methodName]
}

type identifierComments struct {
	typeName    string
	typeComment string
	constants   map[string]string
}

func (self *identifierComments) addConstant(constantValue string, comment string) *identifierComments {
	if comment == "" {
		return self
	}
	if self.constants == nil {
		self.constants = make(map[string]string)
	}
	self.constants[constantValue] = comment
	return self
}

func (self *identifierComments) getConstant(constantValue string) string {
	if self == nil {
		return ""
	}
	return self.constants[constantValue]
}

type packageComments struct {
	packagePath string

	structs     map[string]*structComments
	interfaces  map[string]*interfaceComments
	identifiers map[string]*identifierComments
}

func (self *packageComments) addStruct(typeName, typeComment string) *structComments {
	if self.structs == nil {
		self.structs = make(map[string]*structComments)
	}
	if _, ok := self.structs[typeName]; !ok {
		self.structs[typeName] = &structComments{
			typeName:    typeName,
			typeComment: typeComment,
		}
	}
	return self.structs[typeName]
}

func (self *packageComments) addInterface(typeName, typeComment string) *interfaceComments {
	if self.interfaces == nil {
		self.interfaces = make(map[string]*interfaceComments)
	}
	if _, ok := self.interfaces[typeName]; !ok {
		self.interfaces[typeName] = &interfaceComments{
			typeName:    typeName,
			typeComment: typeComment,
		}
	}
	return self.interfaces[typeName]
}

func (self *packageComments) addIdentifier(typeName, typeComment string) *identifierComments {
	if self.identifiers == nil {
		self.identifiers = make(map[string]*identifierComments)
	}
	if _, ok := self.identifiers[typeName]; !ok {
		self.identifiers[typeName] = &identifierComments{
			typeName:    typeName,
			typeComment: typeComment,
		}
	}
	return self.identifiers[typeName]
}

func (self *packageComments) getStruct(typeName string) *structComments {
	if self == nil {
		return nil
	}
	return self.structs[typeName]
}

func (self *packageComments) getInterface(typeName string) *interfaceComments {
	if self == nil {
		return nil
	}
	return self.interfaces[typeName]
}

func (self *packageComments) getIdentifier(typeName string) *identifierComments {
	if self == nil {
		return nil
	}
	return self.identifiers[typeName]
}

type comments struct {
	packages map[string]*packageComments
}

func (self *comments) addPackage(packagePath string) *packageComments {
	if self.packages == nil {
		self.packages = make(map[string]*packageComments)
	}
	if _, ok := self.packages[packagePath]; !ok {
		self.packages[packagePath] = &packageComments{
			packagePath: packagePath,
		}
	}
	return self.packages[packagePath]
}

func (self *comments) getPackage(packagePath string) *packageComments {
	return self.packages[packagePath]
}

func (self *comments) generateCode() string {
	var lines []string //nolint:prealloc
	lines = append(lines, "package commentparse")
	lines = append(lines, "")
	lines = append(lines, "// Generated code, do not edit or commit.")
	lines = append(lines, "func init() {")
	for _, packageComments := range self.packages {
		lines = append(lines, fmt.Sprintf("\t// %s", packageComments.packagePath))
		// write struct comments
		for _, structComments := range packageComments.structs {
			if structComments.typeComment == "" && len(structComments.fields) == 0 && len(structComments.methods) == 0 {
				continue
			}
			var parts []string
			parts = append(parts, fmt.Sprintf("addPackage(%q)", packageComments.packagePath))
			parts = append(parts, fmt.Sprintf("addStruct(%q, %q)", structComments.typeName, structComments.typeComment))
			for fieldName, fieldComment := range structComments.fields {
				parts = append(parts, fmt.Sprintf("addField(%q, %q)", fieldName, fieldComment))
			}
			for methodName, methodComment := range structComments.methods {
				parts = append(parts, fmt.Sprintf("addMethod(%q, %q)", methodName, methodComment))
			}
			lines = append(lines, fmt.Sprintf("\t// %s", structComments.typeName))
			lines = append(lines, "\tparsedComments.\n\t\t"+strings.Join(parts, ".\n\t\t"))
			lines = append(lines, "")
		}
		// write interface comments
		for _, interfaceComments := range packageComments.interfaces {
			if interfaceComments.typeComment == "" && len(interfaceComments.methods) == 0 {
				continue
			}
			var parts []string
			parts = append(parts, fmt.Sprintf("addPackage(%q)", packageComments.packagePath))
			parts = append(parts, fmt.Sprintf("addInterface(%q, %q)", interfaceComments.typeName, interfaceComments.typeComment))
			for methodName, methodComment := range interfaceComments.methods {
				parts = append(parts, fmt.Sprintf("addMethod(%q, %q)", methodName, methodComment))
			}
			lines = append(lines, fmt.Sprintf("\t// %s", interfaceComments.typeName))
			lines = append(lines, "\tparsedComments.\n\t\t"+strings.Join(parts, ".\n\t\t"))
			lines = append(lines, "")
		}
		// write identifier comments
		for _, identifierComments := range packageComments.identifiers {
			if identifierComments.typeComment == "" && len(identifierComments.constants) == 0 {
				continue
			}
			var parts []string
			parts = append(parts, fmt.Sprintf("addPackage(%q)", packageComments.packagePath))
			parts = append(parts, fmt.Sprintf("addIdentifier(%q, %q)", identifierComments.typeName, identifierComments.typeComment))
			for constantName, constantComment := range identifierComments.constants {
				parts = append(parts, fmt.Sprintf("addConstant(%q, %q)", constantName, constantComment))
			}
			lines = append(lines, fmt.Sprintf("\t// %s", identifierComments.typeName))
			lines = append(lines, "\tparsedComments.\n\t\t"+strings.Join(parts, ".\n\t\t"))
			lines = append(lines, "")
		}
	}
	lines = append(lines, "}")
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

// Walk through all top level type declarations in a package
func walkTypes(parsedPackage *packages.Package, callback func(string, string, *ast.TypeSpec)) {
	for _, parsedSyntax := range parsedPackage.Syntax {
		for _, declaration := range parsedSyntax.Decls {
			genDeclaration, ok := declaration.(*ast.GenDecl)
			if !ok || genDeclaration.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDeclaration.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				typeName := typeSpec.Name.String()
				callback(typeName, trimComment(genDeclaration.Doc), typeSpec)
			}
		}
	}
}

// Walk through all top level const declarations in a package
func walkConstants(parsedPackage *packages.Package, callback func(string, string, string, ast.Expr)) {
	for _, parsedSyntax := range parsedPackage.Syntax {
		for _, declaration := range parsedSyntax.Decls {
			genDeclaration, ok := declaration.(*ast.GenDecl)
			if !ok || genDeclaration.Tok != token.CONST {
				continue
			}
			for _, spec := range genDeclaration.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if len(valueSpec.Values) != len(valueSpec.Names) {
					continue
				}
				// process consts within a const group
				for index, specName := range valueSpec.Names {
					constantName := specName.Name
					if !token.IsExported(constantName) {
						continue
					}
					var typeName string
					if identifier, ok := valueSpec.Type.(*ast.Ident); ok {
						typeName = identifier.Name
					}
					// first check the immediate comment on the value
					typeComment := trimComment(valueSpec.Doc)
					if typeComment == "" {
						// otherwise use the comment on the declaration group
						typeComment = trimComment(genDeclaration.Doc)
					}
					callback(typeName, constantName, typeComment, valueSpec.Values[index])
				}
			}
		}
	}
}

// Walk through member function declarations in a package
func walkMethods(parsedPackage *packages.Package, callback func(string, string, string)) {
	for _, parsedSyntax := range parsedPackage.Syntax {
		for _, declaration := range parsedSyntax.Decls {
			funcDeclaration, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			methodName := funcDeclaration.Name.String()
			if !token.IsExported(methodName) {
				continue
			}
			var typeName string
			if funcDeclaration.Recv != nil && len(funcDeclaration.Recv.List) > 0 {
				switch typeValue := funcDeclaration.Recv.List[0].Type.(type) {
				case *ast.Ident:
					typeName = typeValue.String()
				case *ast.StarExpr:
					if ident, ok := typeValue.X.(*ast.Ident); ok {
						typeName = ident.String()
					}
				}
			}
			callback(typeName, methodName, trimComment(funcDeclaration.Doc))
		}
	}
}

var parsedComments = &comments{}

const loadMode = packages.NeedName |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedTypesSizes

// Parse all go code specified by pattern, returns generated comments for comment indexing
//
//nolint:revive
func Parse(pattern string, includeTests bool) *comments {
	parsedPackages, err := packages.Load(&packages.Config{
		Fset:  token.NewFileSet(),
		Mode:  loadMode,
		Tests: includeTests,
	}, pattern)
	if err != nil {
		panic(fmt.Errorf("commentparse: %w", err))
	}
	parsedComments = &comments{} // reset all comments
	for _, parsedPackage := range parsedPackages {
		packageComments := parsedComments.addPackage(parsedPackage.PkgPath)
		mapParentTypeNames := make(map[string]string)
		walkTypes(parsedPackage, func(typeName, typeComment string, typeSpec *ast.TypeSpec) {
			switch typeValue := typeSpec.Type.(type) {
			// top level structs
			case *ast.StructType:
				_ = packageComments.addStruct(typeName, typeComment)
			// top level interfaces
			case *ast.InterfaceType:
				_ = packageComments.addInterface(typeName, typeComment)
				for _, field := range typeValue.Methods.List {
					if len(field.Names) > 0 {
						continue
					}
					if ident, ok := field.Type.(*ast.Ident); ok {
						mapParentTypeNames[ident.String()] = typeName
					}
				}
			// top level identifiers
			case *ast.Ident:
				_ = packageComments.addIdentifier(typeName, typeComment)
			}
		})
		walkConstants(parsedPackage, func(typeName, constantName, constantComment string, value ast.Expr) {
			if typeName == "" {
				return
			}
			literal, ok := value.(*ast.BasicLit)
			if !ok {
				return
			}
			// stringify the literal value
			var literalValue string
			switch literal.Kind {
			case token.INT, token.FLOAT, token.IMAG:
				literalValue = literal.Value
			case token.CHAR, token.STRING:
				var err error
				if literalValue, err = strconv.Unquote(literal.Value); err != nil {
					return
				}
			}
			if identifier := packageComments.getIdentifier(typeName); identifier != nil {
				identifier.addConstant(literalValue, constantComment)
			}
		})
		walkTypes(parsedPackage, func(typeName, typeComment string, typeSpec *ast.TypeSpec) {
			switch typeValue := typeSpec.Type.(type) {
			// top level structs
			case *ast.StructType:
				for _, field := range typeValue.Fields.List {
					if len(field.Names) > 0 {
						if fieldName := field.Names[0].Name; token.IsExported(fieldName) {
							packageComments.getStruct(typeName).addField(fieldName, trimComment(field.Doc))
						}
					}
				}
			// top level interfaces
			case *ast.InterfaceType:
				for _, field := range typeValue.Methods.List {
					if len(field.Names) > 0 {
						if fieldName := field.Names[0].Name; token.IsExported(fieldName) {
							packageComments.getInterface(typeName).addMethod(fieldName, trimComment(field.Doc))
							if parentTypeName, ok := mapParentTypeNames[typeName]; ok {
								packageComments.getInterface(parentTypeName).addMethod(fieldName, trimComment(field.Doc))
							}
						}
					}
				}
			}
		})
		walkMethods(parsedPackage, func(typeName, methodName, methodComment string) {
			if typeName == "" {
				return
			}
			if structComments := packageComments.getStruct(typeName); structComments != nil {
				structComments.addMethod(methodName, methodComment)
			}
		})
	}
	return parsedComments
}

// GetStructComment returns the comment for a struct.
func GetStructComment(packagePath, typeName string) string {
	if structComments := parsedComments.getPackage(packagePath).getStruct(typeName); structComments != nil {
		return structComments.typeComment
	}
	return ""
}

// GetStructFieldComment returns the comment for a field in a struct.
func GetStructFieldComment(packagePath, typeName, fieldName string) string {
	return parsedComments.getPackage(packagePath).getStruct(typeName).getField(fieldName)
}

// GetStructMethodComment returns the comment for a method in a struct.
func GetStructMethodComment(packagePath, typeName, methodName string) string {
	return parsedComments.getPackage(packagePath).getStruct(typeName).getMethod(methodName)
}

// GetInterfaceComment returns the comment for an interface.
func GetInterfaceComment(packagePath, typeName string) string {
	if interfaceComments := parsedComments.getPackage(packagePath).getInterface(typeName); interfaceComments != nil {
		return interfaceComments.typeComment
	}
	return ""
}

// GetInterfaceMethodComment returns the comment for a method in an interface.
func GetInterfaceMethodComment(packagePath, typeName, methodName string) string {
	return parsedComments.getPackage(packagePath).getInterface(typeName).getMethod(methodName)
}

// GetIdentifierComment returns the comment for an identifier.
func GetIdentifierComment(packagePath, typeName string) string {
	if identifierComments := parsedComments.getPackage(packagePath).getIdentifier(typeName); identifierComments != nil {
		return identifierComments.typeComment
	}
	return ""
}

// GetIdentifierConstantComment returns the comment for a constant value of an identifier.
func GetIdentifierConstantComment(packagePath, typeName string, constantValue any) string {
	value := fmt.Sprintf("%v", constantValue) // stringify
	return parsedComments.getPackage(packagePath).getIdentifier(typeName).getConstant(value)
}
