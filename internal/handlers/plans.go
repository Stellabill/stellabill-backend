package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Plan struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Amount      string `json:"amount"`
	Currency    string `json:"currency"`
	Interval    string `json:"interval"`
	Description string `json:"description,omitempty"`
}

func ListPlans(c *gin.Context) {
	// TODO: load from DB, filter by merchant
	plans := []Plan{}
	c.JSON(http.StatusOK, gin.H{"plans": plans})
}
