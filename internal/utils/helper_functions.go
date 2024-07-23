package utils

import (
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Compare compares two structs and returns a map of differences
func EqualListValues(a, b basetypes.ListValue) bool {
	if len(a.Elements()) != len(b.Elements()) {
		return false
	}
	for i := range a.Elements() {
		if a.Elements()[i] != b.Elements()[i] {
			return false
		}
	}
	return true
}

func ConvertToStringSlice(values []attr.Value) []string {
	var result []string
	for _, v := range values {
		result = append(result, strings.Replace(v.String(), "\"", "", -1))
	}
	return result
}

func ConvertInterfaceToTypesList(values []interface{}) types.List {
	// Create a slice to hold the types.Value elements
	elements := make([]attr.Value, len(values))

	// Iterate over the Go slice and convert each string to types.String
	for i, s := range values {
		elements[i] = types.StringValue(s.(string))
	}

	// Create the types.List with the elements
	list := types.ListValueMust(types.StringType, elements)

	return list
}

func ConvertStringsArrayToTypesList(values []string) types.List {
	// Create a slice to hold the types.Value elements
	elements := make([]attr.Value, len(values))

	// Iterate over the Go slice and convert each string to types.String
	for i, s := range values {
		elements[i] = types.StringValue(s)
	}

	// Create the types.List with the elements
	list := types.ListValueMust(types.StringType, elements)

	return list
}
