package store

import (
	"reflect"
	"testing"
)

func TestSQLQueriesLoaded(t *testing.T) {
	// Why: Use reflection to iterate through all fields of the SQL struct and ensure 
	// they have been properly populated from the .sql files. This catches manual 
	// assignment omissions in queries.go during development.
	
	v := reflect.ValueOf(SQL)
	typeOfSQL := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldName := typeOfSQL.Field(i).Name
		
		if field.Kind() == reflect.String {
			if field.String() == "" {
				t.Errorf("SQL field %q is empty. Ensure '-- name: %s' exists in the corresponding .sql file and is assigned in loadAllQueries()", fieldName, fieldName)
			}
		}
	}
}
