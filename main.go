package main

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		panic("environmental variable file not found")
	}
	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		panic("redis connection failed")
	}
	rdb := redis.NewClient(opt)
	connStr := os.Getenv("POSTGRES_URL")
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		panic("postgres connection failed")
	}
	app := gin.Default()
	app.GET("/", func(c *gin.Context) { indexGet(c, db, rdb) })
	app.GET("/users/:id", func(c *gin.Context) { userGet(c, db, rdb) })
	app.Run("localhost:8000")
}

func indexGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	c.IndentedJSON(http.StatusOK, gin.H{"message": "hello world"})
}

func userGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, hasId := c.Params.Get("id")
	if !hasId {
		c.Status(http.StatusBadRequest)
		return
	}
	var user struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	err := db.QueryRow("SELECT email, COALESCE(name, '') AS name, COALESCE(address, '') AS address FROM Users WHERE id = "+id+";").Scan(&user.Email, &user.Name, &user.Address)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.IndentedJSON(http.StatusOK, user)
}
