package ginfixture

import "github.com/gin-gonic/gin"

func Routes(engine *gin.Engine) {
	admin := engine.Group("/admin")
	admin.Use(auditMiddleware())
	admin.GET("/users", listUsers)
}

func auditMiddleware() gin.HandlerFunc { return func(*gin.Context) {} }
func listUsers(*gin.Context)           {}
