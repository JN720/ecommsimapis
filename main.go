package main

import (
	"database/sql"
	"net/http"
	"os"
	"strconv"

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
	app.POST("/products", func(c *gin.Context) { productPost(c, db, rdb) })
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

func productPost(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var product struct {
		Card        string `json:"card"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Quantity    string `json:"quantity"`
		Price       string `json:"price"`
	}

	if err := c.ShouldBindJSON(&product); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	quantity, err1 := strconv.ParseInt(product.Quantity, 10, 16)
	id, err2 := strconv.ParseInt(product.Card, 10, 32)
	if err1 != nil || err2 != nil || quantity < 1 || id < 1 {
		c.Status(http.StatusBadRequest)
		return
	}
	_, err := db.Query("INSERT INTO Products(card_id, name, description, quantity, price, status, created) VALUES(" +
		product.Card + ",'" + product.Name + "', '" + product.Description + "', " + product.Quantity + ", " + product.Price + ", 'A', NOW());")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusCreated)
}
