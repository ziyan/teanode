package finance

// GraphqlOperationOf is the finance area's operation a tool operation
// calls, empty for the ones that span calls or answer themselves; false
// when there is no such tool operation. For the parity test.
func GraphqlOperationOf(name string) (string, bool) {
	operation, isKnown := operations[name]
	if !isKnown {
		return "", false
	}
	return operation.graphqlOperation, true
}

// AcceptedArgumentsOf is every argument a tool operation reads.
func AcceptedArgumentsOf(name string) []string {
	return acceptedArguments(operations[name])
}
