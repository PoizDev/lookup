package gormfixture

import "gorm.io/gorm"

func Queries(db *gorm.DB, id string) {
	db.Raw("SELECT * FROM users WHERE id = ?", id)
	db.Raw("SELECT * FROM users WHERE id = " + id)
}
