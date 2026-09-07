package sqlinjection

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Safe(c *gin.Context, db *gorm.DB) {
	id := c.Query("id")
	db.Raw("SELECT * FROM users WHERE id = ?", id)
	db.Where(map[string]any{"id": id}).First(&struct{}{})
}
