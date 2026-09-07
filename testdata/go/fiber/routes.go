package fiberfixture

import "github.com/gofiber/fiber/v3"

func Routes(app *fiber.App) {
	admin := app.Group("/admin")
	admin.Use(auditMiddleware)
	admin.Get("/users", listUsers)
}

func auditMiddleware(c fiber.Ctx) error { return c.Next() }
func listUsers(c fiber.Ctx) error       { return nil }
