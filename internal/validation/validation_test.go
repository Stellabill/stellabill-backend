package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateStruct(t *testing.T) {
	type TestStruct struct {
		ID    string   `json:"id" validate:"required,uuid"`
		Email string   `json:"email" validate:"required,email"`
		Age   int      `json:"age" validate:"min=18,max=99"`
		Tags  []string `json:"tags" validate:"required,dive,min=3"`
	}

	t.Run("valid struct", func(t *testing.T) {
		s := TestStruct{
			ID:    "550e8400-e29b-41d4-a716-446655440000",
			Email: "test@example.com",
			Age:   25,
			Tags:  []string{"tag1", "tag2", "tag3"},
		}
		errs := ValidateStruct(s)
		assert.Nil(t, errs)
	})

	t.Run("invalid struct fields", func(t *testing.T) {
		s := TestStruct{
			ID:    "invalid-uuid",
			Email: "invalid-email",
			Age:   10,
			Tags:  []string{"sh"},
		}
		errs := ValidateStruct(s)
		assert.Len(t, errs, 4)

		fieldMap := make(map[string]string)
		for _, e := range errs {
			fieldMap[e.Field] = e.Message
		}

		assert.Contains(t, fieldMap["id"], "must be a valid UUID")
		assert.Contains(t, fieldMap["email"], "must be a valid email address")
		assert.Contains(t, fieldMap["age"], "must be at least 18")
		assert.Contains(t, fieldMap["tags[0]"], "must be at least 3")
	})
}

func TestValidateUUID(t *testing.T) {
	t.Run("valid UUID", func(t *testing.T) {
		err := ValidateUUID("550e8400-e29b-41d4-a716-446655440000")
		assert.NoError(t, err)
	})

	t.Run("invalid UUID", func(t *testing.T) {
		err := ValidateUUID("not-a-uuid")
		assert.Error(t, err)
	})

	t.Run("empty UUID", func(t *testing.T) {
		err := ValidateUUID("")
		assert.Error(t, err)
	})
}

func TestValidateVar(t *testing.T) {
	t.Run("valid slice with dive", func(t *testing.T) {
		slice := []string{"active", "pending"}
		errs := ValidateVar(slice, "required,dive,oneof=active pending cancelled")
		assert.Nil(t, errs)
	})

	t.Run("invalid slice element", func(t *testing.T) {
		slice := []string{"active", "invalid"}
		errs := ValidateVar(slice, "required,dive,oneof=active pending cancelled")
		assert.Len(t, errs, 1)
		assert.Equal(t, "[1]", errs[0].Field)
	})
}
