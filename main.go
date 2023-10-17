package main

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"strconv"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		panic("environmental variable file not found")
	}
	fb, err := firebase.NewApp(context.Background(), nil)
	if err != nil {
		panic("firebase connection failed")
	}
	fba, err := fb.Auth(context.Background())
	if err != nil {
		panic("firebase auth failed")
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

	authMW := func(c *gin.Context) {
		authenticate(c, fba)
	}

	//api status
	app.GET("/", func(c *gin.Context) { indexGet(c, db, rdb) })
	//public user info
	app.GET("/users/:id", func(c *gin.Context) { userGet(c, db, rdb) })
	//user profile
	app.PATCH("/users/:id", authMW, func(c *gin.Context) { userPatch(c, db, rdb) })
	//user cards
	app.GET("/cards/:id", authMW, func(c *gin.Context) { cardGet(c, db, rdb) })
	//new card: should have auto generated card id's
	app.POST("/cards", authMW, func(c *gin.Context) { cardPost(c, db, rdb) })
	//product info: should have image retrieval
	app.GET("/products/:id", func(c *gin.Context) { productGet(c, db, rdb) })
	//product creation
	app.POST("/products", authMW, func(c *gin.Context) { productPost(c, db, rdb) })

	port := os.Getenv("PORT")
	if err := app.Run("localhost:" + port); err != nil {
		app.Run("localhost:8000")
	}
}

func authenticate(c *gin.Context, fba *auth.Client) {
	idToken := c.GetHeader("Authorization")
	token, err := fba.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		c.Status(http.StatusUnauthorized)
		c.Abort()
		return
	}
	c.Set("token", token)
	c.Next()
}

func indexGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	c.IndentedJSON(http.StatusOK, gin.H{"message": "Functional"})
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
	err := db.QueryRow("SELECT email, COALESCE(name, '') AS name, COALESCE(address, '') AS address FROM Users WHERE id = "+
		id+";").Scan(&user.Email, &user.Name, &user.Address)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.IndentedJSON(http.StatusOK, user)
}

func userPatch(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var user struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	}
	id, hasId := c.Params.Get("id")
	if !hasId {
		c.Status(http.StatusBadRequest)
		return
	}
	if err := c.ShouldBindJSON(&user); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if _, err := db.Query("UPDATE Users SET name = '" + user.Name + "', address = '" + user.Address + "' WHERE id = " + id + ";"); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

func cardGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var cards []struct {
		Number  string `json:"number"`
		Balance string `json:"balance"`
	}

	id, hasId := c.Params.Get("id")
	if !hasId {
		c.Status(http.StatusBadRequest)
		return
	}

	rows, err := db.Query("SELECT Cards.number AS number, Cards.balance AS balance FROM Users JOIN Cards" +
		" ON Users.id = Cards.user_id WHERE Users.id = " + id + " GROUP BY Cards.number, Cards.balance;")

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	for rows.Next() {
		var card struct {
			Number  string `json:"number"`
			Balance string `json:"balance"`
		}
		if err := rows.Scan(&card.Number, &card.Balance); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		cards = append(cards, card)
	}
	c.IndentedJSON(http.StatusOK, gin.H{"cards": cards})
}

func cardPost(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var card struct {
		User   string `json:"user"`
		Number string `json:"number"`
		Code   string `json:"code"`
	}
	if err := c.ShouldBindJSON(&card); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}
	if len(card.Number) != 12 || len(card.Code) != 4 {
		c.Status(http.StatusBadRequest)
		return
	}
	_, err := db.Query("INSERT INTO Cards(user_id, number, code, balance, created) VALUES(" +
		card.User + ",'" + card.Number + "', '" + card.Code + "', 0, NOW());")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusCreated)
}

func productGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var product struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Department  string `json:"department"`
		Quantity    string `json:"quantity"`
		Price       string `json:"price"`
	}
	id, hasId := c.Params.Get("id")
	if !hasId {
		c.Status(http.StatusBadRequest)
		return
	}
	err := db.QueryRow("SELECT name, description, department, quantity, price FROM Products WHERE id = "+id+
		";").Scan(&product.Name, &product.Description, &product.Department, &product.Quantity, &product.Price)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.IndentedJSON(http.StatusOK, gin.H{"product": product})
}

func productPost(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var product struct {
		Card        string `json:"card"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Department  string `json:"department"`
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
	_, err := db.Query("INSERT INTO Products(card_id, name, description, department, quantity, price, status, created) VALUES(" +
		product.Card + ",'" + product.Name + "', '" + product.Description + "', '" + product.Department + "', " + product.Quantity + ", " + product.Price + ", 'A', NOW());")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusCreated)
}
