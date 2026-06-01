package middleware

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
)

func IPRestrictionMiddleware(allowedCIDRs []string) gin.HandlerFunc {
	var allowedNets []*net.IPNet
	for _, cidr := range allowedCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil {
			allowedNets = append(allowedNets, ipnet)
		}
	}
	return func(c *gin.Context) {
		remoteIP := getClientIP(c)
		ip := net.ParseIP(remoteIP)
		if ip == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		for _, net := range allowedNets {
			if net.Contains(ip) {
				c.Next()
				return
			}
		}
		c.AbortWithStatus(http.StatusForbidden)
	}
}
