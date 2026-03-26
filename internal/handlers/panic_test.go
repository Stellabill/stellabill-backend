package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// TestPanicHandler intentionally panics to test recovery middleware
func TestPanicHandler(c *gin.Context) {
	panicType := c.Query("type")
	switch panicType {
	case "string":
		panic("intentional string panic")
	case "runtime":
		panic(runtimeError("intentional runtime error"))
	case "nil":
		var nilPtr *string
		_ = *nilPtr // nil pointer dereference
	case "custom":
		panic(&customPanic{Message: "custom panic type"})
	default:
		panic("default test panic")
	}
}

type runtimeError string

func (e runtimeError) Error() string {
	return string(e)
}

type customPanic struct {
	Message string
}

func (p *customPanic) String() string {
	return p.Message
}

// PanicAfterWriteHandler tests panic after headers are written
func PanicAfterWriteHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "response written"})
	panic("panic after response written")
}

// NestedPanicHandler tests nested panics in middleware chain
func NestedPanicHandler(c *gin.Context) {
	// Simulate nested panic scenario
	func() {
		defer func() {
			if err := recover(); err != nil {
				// This creates a nested panic situation
				panic("nested panic during recovery")
			}
		}()
		panic("initial panic")
	}()
}
