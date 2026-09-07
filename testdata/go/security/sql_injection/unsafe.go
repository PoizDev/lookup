package sqlinjection

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Unsafe(c *gin.Context, db *gorm.DB) {
	id := c.Query("id")
	db.Raw(fmt.Sprintf("SELECT * FROM users WHERE id = %s", id))
}
