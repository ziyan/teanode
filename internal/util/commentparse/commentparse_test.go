package commentparse

import (
	"testing"
)

const testPackagePath = "github.com/ziyan/teanode/internal/util/commentparse"

// This is a custom string type.
type TestCustomStringType string

const (
	// This is the enum case for string one.
	CaseOneTestCustomStringType TestCustomStringType = "one"

	// This is the enum case for string two.
	CaseTwoTestCustomStringType TestCustomStringType = "two"
)

// This is the enum case for string three.
const CaseThreeTestCustomStringType TestCustomStringType = "three"

// This is a custom int type.
type TestCustomIntType int

const (
	// This is the enum case for int 1.
	Case1TestCustomIntType TestCustomIntType = 1

	// This is the enum case for int 2.
	Case2TestCustomIntType TestCustomIntType = 2
)

// This is the enum case for int 3.
const Case3TestCustomIntType TestCustomIntType = 3

type TestEmptyStruct struct {
}

// This is a base test struct.
type testStructBase struct {
	// This is a base test field.
	BaseTestField string
}

// This is a test method for base test struct.
func (self *testStructBase) TestMethodBase() {
}

// This is a test struct.
//
// More comment
type TestStruct struct {
	testStructBase

	// This is a test field.
	// And it is a string.
	TestField string

	// This is a test field with a custom string type.
	// And it is a TestCustomStringType.
	TestFieldWithTestCustomStringType TestCustomStringType

	// This is a test field with a custom int type.
	// And it is a TestCustomIntType.
	TestFieldWithTestCustomIntType TestCustomIntType
}

// This is a test method for test struct.
func (self *TestStruct) TestMethod() {
}

// This is a test interface.
//
// More comment
type TestInterface interface {
	// This is a test function.
	Test(argument string) bool
}

// This is a parent interface.
type TestParentInterface interface {
	TestInterface
}

func TestParse(t *testing.T) {
	t.Parallel()

	code := Parse(testPackagePath, true).generateCode()
	t.Logf("generated code: %s", code)
	if comment := GetStructComment(testPackagePath, "testStructBase"); comment != "This is a base test struct." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructFieldComment(testPackagePath, "testStructBase", "BaseTestField"); comment != "This is a base test field." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructMethodComment(testPackagePath, "testStructBase", "TestMethodBase"); comment != "This is a test method for base test struct." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructComment(testPackagePath, "TestStruct"); comment != "This is a test struct.\n\nMore comment" {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructFieldComment(testPackagePath, "TestStruct", "TestField"); comment != "This is a test field.\nAnd it is a string." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructFieldComment(testPackagePath, "TestStruct", "TestFieldWithTestCustomStringType"); comment != "This is a test field with a custom string type.\nAnd it is a TestCustomStringType." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructFieldComment(testPackagePath, "TestStruct", "TestFieldWithTestCustomIntType"); comment != "This is a test field with a custom int type.\nAnd it is a TestCustomIntType." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructMethodComment(testPackagePath, "TestStruct", "TestMethod"); comment != "This is a test method for test struct." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetInterfaceComment(testPackagePath, "TestInterface"); comment != "This is a test interface.\n\nMore comment" {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetInterfaceMethodComment(testPackagePath, "TestInterface", "Test"); comment != "This is a test function." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetInterfaceComment(testPackagePath, "TestParentInterface"); comment != "This is a parent interface." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetInterfaceMethodComment(testPackagePath, "TestParentInterface", "Test"); comment != "This is a test function." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructComment(testPackagePath, "TestEmptyStruct"); comment != "" {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetStructComment(testPackagePath, "NotFoundStruct"); comment != "" {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetInterfaceComment(testPackagePath, "NotFoundInterface"); comment != "" {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierComment(testPackagePath, "TestCustomStringType"); comment != "This is a custom string type." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierConstantComment(testPackagePath, "TestCustomStringType", CaseOneTestCustomStringType); comment != "This is the enum case for string one." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierConstantComment(testPackagePath, "TestCustomStringType", CaseTwoTestCustomStringType); comment != "This is the enum case for string two." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierConstantComment(testPackagePath, "TestCustomStringType", CaseThreeTestCustomStringType); comment != "This is the enum case for string three." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierComment(testPackagePath, "TestCustomIntType"); comment != "This is a custom int type." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierConstantComment(testPackagePath, "TestCustomIntType", Case1TestCustomIntType); comment != "This is the enum case for int 1." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierConstantComment(testPackagePath, "TestCustomIntType", Case2TestCustomIntType); comment != "This is the enum case for int 2." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
	if comment := GetIdentifierConstantComment(testPackagePath, "TestCustomIntType", Case3TestCustomIntType); comment != "This is the enum case for int 3." {
		t.Fatalf("unexpected %q: %s", comment, code)
	}
}
