package apigraph

import (
	"fmt"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/lexer"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"

	"github.com/ziyan/teanode/internal/config"
)

// prepareGraphRequest does the document work before acquiring a database
// connection. Execution receives the validated tree and must not parse again.
func (self *graph) prepareGraphRequest(request *graphRequest) (graphql.ExecuteParams, *graphql.Result) {
	var limits config.GraphQL
	if self.config != nil {
		limits = self.config.Current().GraphQL
	} else {
		limits = config.Default().GraphQL
	}
	requestSource := source.NewSource(&source.Source{Body: []byte(request.Query), Name: "GraphQL request"})
	if err := checkGraphTokens(requestSource, limits); err != nil {
		return graphql.ExecuteParams{}, &graphql.Result{Errors: gqlerrors.FormatErrors(err)}
	}
	document, err := parser.Parse(parser.ParseParams{Source: requestSource})
	if err != nil {
		return graphql.ExecuteParams{}, &graphql.Result{Errors: gqlerrors.FormatErrors(err)}
	}
	if err := checkGraphSelections(document, limits); err != nil {
		return graphql.ExecuteParams{}, &graphql.Result{Errors: gqlerrors.FormatErrors(err)}
	}
	// Check cycles separately before rules that recursively compare fragments.
	cycles := graphql.ValidateDocument(&self.schema, document, []graphql.ValidationRuleFn{graphql.NoFragmentCyclesRule})
	if !cycles.IsValid {
		return graphql.ExecuteParams{}, &graphql.Result{Errors: cycles.Errors}
	}
	validation := graphql.ValidateDocument(&self.schema, document, nil)
	if !validation.IsValid {
		return graphql.ExecuteParams{}, &graphql.Result{Errors: validation.Errors}
	}
	if _, err := selectGraphOperation(document, request.OperationName); err != nil {
		return graphql.ExecuteParams{}, &graphql.Result{Errors: gqlerrors.FormatErrors(err)}
	}
	return graphql.ExecuteParams{
		Schema: self.schema, AST: document,
		OperationName: request.OperationName, Args: request.Variables,
	}, nil
}

// Lex first so deeply nested input cannot reach the recursive parser. Quoted
// strings and comments are tokens, not delimiters to count as nesting.
func checkGraphTokens(requestSource *source.Source, limits config.GraphQL) error {
	if len(requestSource.Body) > maximumRequestSize {
		return fmt.Errorf("GraphQL document exceeds maximum size")
	}
	nextToken := lexer.Lex(requestSource)
	depth := 0
	for tokenCount := 0; ; tokenCount++ {
		token, err := nextToken(0)
		if err != nil {
			return err
		}
		if token.Kind == lexer.EOF {
			return nil
		}
		if tokenCount >= limits.MaximumTokenCount {
			return fmt.Errorf("GraphQL document exceeds maximum token count")
		}
		switch token.Kind {
		case lexer.BRACE_L, lexer.BRACKET_L, lexer.PAREN_L:
			depth++
			if depth > limits.MaximumDepth {
				return fmt.Errorf("GraphQL document exceeds maximum depth")
			}
		case lexer.BRACE_R, lexer.BRACKET_R, lexer.PAREN_R:
			depth--
		}
	}
}

func checkGraphSelections(document *ast.Document, limits config.GraphQL) error {
	fragments := make(map[string]*ast.FragmentDefinition)
	for _, definition := range document.Definitions {
		if fragment, isFragment := definition.(*ast.FragmentDefinition); isFragment {
			fragments[fragment.Name.Value] = fragment
		}
	}
	selectionCount := 0
	activeFragments := make(map[string]bool)
	var walkSelections func(*ast.SelectionSet, int) error
	walkSelections = func(selections *ast.SelectionSet, depth int) error {
		if selections == nil {
			return nil
		}
		if depth > limits.MaximumDepth {
			return fmt.Errorf("GraphQL document exceeds maximum expanded depth")
		}
		for _, selection := range selections.Selections {
			selectionCount++
			if selectionCount > limits.MaximumSelectionCount {
				return fmt.Errorf("GraphQL document exceeds maximum expanded selection count")
			}
			switch selected := selection.(type) {
			case *ast.Field:
				if err := walkSelections(selected.SelectionSet, depth+1); err != nil {
					return err
				}
			case *ast.InlineFragment:
				if err := walkSelections(selected.SelectionSet, depth+1); err != nil {
					return err
				}
			case *ast.FragmentSpread:
				fragmentName := selected.Name.Value
				if activeFragments[fragmentName] {
					continue
				}
				if fragment := fragments[fragmentName]; fragment != nil {
					activeFragments[fragmentName] = true
					err := walkSelections(fragment.SelectionSet, depth+1)
					delete(activeFragments, fragmentName)
					if err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	for _, definition := range document.Definitions {
		switch defined := definition.(type) {
		case *ast.OperationDefinition:
			if err := walkSelections(defined.SelectionSet, 1); err != nil {
				return err
			}
		case *ast.FragmentDefinition:
			if err := walkSelections(defined.SelectionSet, 1); err != nil {
				return err
			}
		}
	}
	return nil
}

func selectGraphOperation(document *ast.Document, operationName string) (*ast.OperationDefinition, error) {
	var operation *ast.OperationDefinition
	for _, definition := range document.Definitions {
		switch defined := definition.(type) {
		case *ast.OperationDefinition:
			if operationName == "" && operation != nil {
				return nil, gqlerrors.NewFormattedError("Must provide operation name if query contains multiple operations.")
			}
			if operationName == "" || defined.Name != nil && defined.Name.Value == operationName {
				operation = defined
			}
		case *ast.FragmentDefinition:
		default:
			return nil, fmt.Errorf("GraphQL cannot execute a request containing a %v", definition.GetKind())
		}
	}
	if operation == nil {
		if operationName != "" {
			return nil, gqlerrors.NewFormattedError(fmt.Sprintf("Unknown operation named %q.", operationName))
		}
		return nil, gqlerrors.NewFormattedError("Must provide an operation.")
	}
	return operation, nil
}
