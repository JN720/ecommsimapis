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
		//DEVELOPMENT_ONLY_AUTHENTICATION_TEST(c, false)
		authenticate(c, fba, false)
	}

	optAuthMW := func(c *gin.Context) {
		//DEVELOPMENT_ONLY_AUTHENTICATION_TEST(c, true)
		authenticate(c, fba, true)
	}

	statusMW := func(c *gin.Context) {
		checkStatus(c, db, rdb)
	}

	//api status
	app.GET("/", func(c *gin.Context) { indexGet(c, db, rdb) })
	//public user info
	app.GET("/users/:id", func(c *gin.Context) { userGet(c, db, rdb) })
	//unban user
	app.PUT("/users/:id", authMW, statusMW, func(c *gin.Context) { userPut(c, db, rdb) })
	//user profile
	app.PATCH("/users", authMW, func(c *gin.Context) { userPatch(c, db, rdb) })
	//ban user
	app.DELETE("/users/:id", authMW, statusMW, func(c *gin.Context) { userDelete(c, db, rdb) })
	//user cards
	app.GET("/cards", authMW, func(c *gin.Context) { cardGet(c, db, rdb) })
	//new card: should have auto generated card id's
	app.POST("/cards", authMW, func(c *gin.Context) { cardPost(c, db, rdb) })
	//product info: should have image retrieval
	app.GET("/products/:id", optAuthMW, func(c *gin.Context) { productGet(c, db, rdb) })
	//manual search
	app.GET("/products", optAuthMW, func(c *gin.Context) { productSearch(c, db, rdb) })
	//product creation
	app.POST("/products", authMW, func(c *gin.Context) { productPost(c, db, rdb) })
	//change product's visibility
	app.PUT("/products/:id", authMW, func(c *gin.Context) { productPut(c, db, rdb) })
	//change product's stock
	app.PATCH("/products/:id", authMW, func(c *gin.Context) { productPatch(c, db, rdb) })
	//product deletion (changes the status in the database)
	app.DELETE("/products/:id", authMW, func(c *gin.Context) { productDelete(c, db, rdb) })
	//reviews for product
	app.GET("/reviews/:id", func(c *gin.Context) { reviewGet(c, db, rdb) })
	//make review
	app.POST("/reviews", authMW, func(c *gin.Context) { reviewPost(c, db, rdb) })
	//get purchase history
	app.GET("/orders", authMW, func(c *gin.Context) { orderGet(c, db, rdb) })
	//purchase
	app.POST("/orders", authMW, func(c *gin.Context) { orderPost(c, db, rdb) })
	//view orders to your products
	app.GET("/orders/queue", authMW, func(c *gin.Context) { orderQueueGet(c, db, rdb) })
	//account creation
	app.POST("/signup", func(c *gin.Context) { signup(c, fba) })
	port := os.Getenv("PORT")
	if err := app.Run("localhost:" + port); err != nil {
		app.Run("localhost:8000")
	}
}

func authenticate(c *gin.Context, fba *auth.Client, opt bool) {
	idToken := c.GetHeader("Authorization")
	token, err := fba.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		if opt {
			c.Next()
			return
		}
		c.Status(http.StatusUnauthorized)
		c.Abort()
		return
	}
	c.Set("UID", token.UID)
	c.Next()
}

func DEVELOPMENT_ONLY_AUTHENTICATION_TEST(c *gin.Context, opt bool) {
	idToken := c.GetHeader("Authorization")
	var token auth.Token
	switch idToken {
	case "token1":
		token.UID = "1"
	case "token2":
		token.UID = "2"
	case "modtoken":
		token.UID = "3"
	default:
		if opt {
			c.Next()
			return
		}
		c.Status(http.StatusUnauthorized)
		c.Abort()
		return
	}
	c.Set("uid", token.UID)
	c.Next()
}

func checkStatus(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		c.Abort()
		return
	}
	var status string
	if status, err := rdb.HGet(context.Background(), id.(string), "status").Result(); err == nil {
		c.Set("status", status)
	} else if err := db.QueryRow("SELECT status FROM Users WHERE id = " + id.(string) + ";").Scan(&status); err == nil {
		c.Set("status", status)
		rdb.HSet(context.Background(), id.(string), map[string]string{"status": status})
	} else {
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if status == "B" {
		c.Status(http.StatusUnauthorized)
		c.Abort()
		return
	}
	c.Next()
}

func signup(c *gin.Context, fba *auth.Client) {
	var credentials struct {
		Email    string `json:"email" binding:"required,email"`
		Password string `json:"password" binding:"required"`
		Name     string `json:"name" binding:"required"`
		Phone    string `json:"phone"`
		//Phone string `json:"phone" binding:"e164"`
	}
	if err := c.BindJSON(&credentials); err != nil {
		return
	}

	params := (&auth.UserToCreate{}).
		Email(credentials.Email).
		EmailVerified(false).
		Password(credentials.Password).
		DisplayName(credentials.Name).
		Disabled(false)

	if credentials.Phone != "" {
		params = params.PhoneNumber(credentials.Phone)
	}

	u, err := fba.CreateUser(context.Background(), params)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.IndentedJSON(http.StatusCreated, gin.H{"user": u})
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

func userPut(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	status, exists := c.Get("status")
	if !exists || status != "M" {
		c.Status(http.StatusUnauthorized)
		return
	}

	id, exists := c.Params.Get("id")
	if !exists {
		c.Status(http.StatusBadRequest)
		return
	}

	if _, err := db.Query("UPDATE Users SET status = 'A' WHERE id = " + id + ";"); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	rdb.HSet(context.Background(), id, map[string]string{"status": "A"})
	c.Status(http.StatusOK)
}

func userPatch(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	uid, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var user struct {
		Name    string `json:"name" binding:"required"`
		Address string `json:"address" binding:"required"`
	}

	if err := c.BindJSON(&user); err != nil {
		return
	}
	if _, err := db.Query("UPDATE Users SET name = '" + user.Name + "', address = '" + user.Address + "' WHERE id = " + uid.(string) + ";"); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

func userDelete(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	status, exists := c.Get("status")
	if !exists || status != "M" {
		c.Status(http.StatusUnauthorized)
		return
	}

	id, exists := c.Params.Get("id")
	if !exists {
		c.Status(http.StatusBadRequest)
		return
	}

	if _, err := db.Query("UPDATE Users SET status = 'B' WHERE id = " + id + ";"); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	rdb.HSet(context.Background(), id, map[string]string{"status": "B"})
	c.Status(http.StatusOK)
}

func cardGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	uid, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var cards []struct {
		Number  string `json:"number"`
		Balance string `json:"balance"`
	}

	rows, err := db.Query("SELECT Cards.number AS number, Cards.balance AS balance FROM Users JOIN Cards" +
		" ON Users.id = Cards.user_id WHERE Users.id = " + uid.(string) + " GROUP BY Cards.number, Cards.balance;")

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
	uid, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var card struct {
		Number string `json:"number" binding:"required,len=12"`
		Code   string `json:"code" binding:"required,len=4"`
	}
	if err := c.BindJSON(&card); err != nil {
		return
	}

	_, err := db.Query("INSERT INTO Cards(user_id, number, code, balance, created) VALUES(" +
		uid.(string) + ",'" + card.Number + "', '" + card.Code + "', 0, NOW());")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusCreated)
}

func productSearch(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	search := ""
	sort := c.Query("sort")
	if sort == "" {
		sort = "created"
	}
	sortType := " DESC"
	if c.Query("sortType") == "1" {
		sortType = " ASC"
	}
	for _, term := range [...]string{"name", "description", "department"} {
		value := c.Query(term)
		if value != "" {
			search += " AND " + term + " LIKE '%" + value + "%'"
		}
	}

	var products []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Department  string `json:"department"`
		Quantity    string `json:"quantity"`
		Price       string `json:"price"`
	}

	rows, err := db.Query("SELECT name, description, department, quantity, price FROM Products" +
		" WHERE status = 'A'" + search + " ORDER BY " + sort + sortType + " LIMIT 50;")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	for rows.Next() {
		var product struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Department  string `json:"department"`
			Quantity    string `json:"quantity"`
			Price       string `json:"price"`
		}
		if err := rows.Scan(&product.Name, &product.Description, &product.Department, &product.Quantity, &product.Price); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		products = append(products, product)
	}
	c.IndentedJSON(http.StatusOK, gin.H{"products": products})
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
	var cardId string
	var status string
	err := db.QueryRow("SELECT card_id, name, description, department, quantity, price, status FROM Products WHERE id = "+id+
		";").Scan(&cardId, &product.Name, &product.Description, &product.Department, &product.Quantity, &product.Price, &status)
	if err != nil || status != "A" {
		c.Status(http.StatusNotFound)
		return
	}
	_, exists := c.Get("uid")
	if !exists {
		c.IndentedJSON(http.StatusOK, gin.H{"product": product})
		return
	}
	var seller struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	err = db.QueryRow("SELECT Users.name AS name, Users.email AS email FROM Users JOIN Cards"+
		" ON Users.id = Cards.user_id WHERE Cards.id ="+cardId+"").Scan(&seller.Name, &seller.Email)
	if err != nil {
		c.IndentedJSON(http.StatusOK, gin.H{"product": product})
		return
	}
	c.IndentedJSON(http.StatusOK, gin.H{"product": product, "seller": seller})
}

func productPost(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var product struct {
		Card        string `json:"card" binding:"required,len=12"`
		Code        string `json:"code" binding:"required,len=4"`
		Name        string `json:"name" binding:"required"`
		Description string `json:"description" binding:"required"`
		Department  string `json:"department" binding:"required"`
		Quantity    string `json:"quantity" binding:"required"`
		Price       string `json:"price" binding:"required"`
	}
	if err := c.BindJSON(&product); err != nil {
		return
	}
	var cardId string
	var code string
	cardErr := db.QueryRow("SELECT id, code FROM Cards WHERE user_id = "+
		id.(string)+" AND number = '"+product.Card+"';").Scan(&cardId, &code)

	if cardErr != nil {
		c.Status(http.StatusNotFound)
	}
	if code != product.Code {
		c.Status(http.StatusUnauthorized)
		return
	}

	quantity, qErr := strconv.ParseInt(product.Quantity, 10, 16)
	if qErr != nil || quantity < 1 {
		c.Status(http.StatusBadRequest)
		return
	}

	_, err := db.Query("INSERT INTO Products(card_id, name, description, department, quantity, price, status, created) VALUES('" +
		cardId + "','" + product.Name + "', '" + product.Description + "', '" + product.Department + "', " + product.Quantity + ", " + product.Price + ", 'A', NOW());")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusCreated)
}

func productPut(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	productId, exists := c.Params.Get("id")
	if !exists {
		c.Status(http.StatusBadRequest)
		return
	}
	var product struct {
		Code string `json:"code" binding:"required,len=4"`
	}
	if err := c.BindJSON(&product); err != nil {
		return
	}
	var code string
	err := db.QueryRow("SELECT Cards.code FROM Products JOIN Cards ON Products.card_id = Cards.id" +
		" WHERE Cards.user_id = " + id.(string) + " AND Products.id = " + productId + ";").Scan(&code)

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if code != product.Code {
		c.Status(http.StatusUnauthorized)
		return
	}

	_, err = db.Query("UPDATE Products SET status = 'A' WHERE id = " + productId + ";")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

func productPatch(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	productId, exists := c.Params.Get("id")
	if !exists {
		c.Status(http.StatusBadRequest)
		return
	}
	var product struct {
		Code     string `json:"code" binding:"required,len=4"`
		Quantity string `json:"quantity" binding:"required,number"`
	}
	if err := c.BindJSON(&product); err != nil {
		return
	}
	var code string
	err := db.QueryRow("SELECT Cards.code FROM Products JOIN Cards ON Products.card_id = Cards.id" +
		" WHERE Cards.user_id = " + id.(string) + " AND Products.id = " + productId + ";").Scan(&code)

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if code != product.Code {
		c.Status(http.StatusUnauthorized)
		return
	}

	if quantity, err := strconv.ParseInt(product.Quantity, 10, 16); err != nil || quantity < 0 {
		c.Status(http.StatusBadRequest)
		return
	}

	_, err = db.Query("UPDATE Products SET quantity = " + product.Quantity + " WHERE id = " + productId + ";")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

func productDelete(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	productId, exists := c.Params.Get("id")
	if !exists {
		c.Status(http.StatusBadRequest)
		return
	}
	var product struct {
		Code string `json:"code" binding:"required,len=4"`
	}
	if err := c.BindJSON(&product); err != nil {
		return
	}
	var code string
	err := db.QueryRow("SELECT Cards.code FROM Products JOIN Cards ON Products.card_id = Cards.id" +
		" WHERE Cards.user_id = " + id.(string) + " AND Products.id = " + productId + ";").Scan(&code)

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if code != product.Code {
		c.Status(http.StatusUnauthorized)
		return
	}

	_, err = db.Query("UPDATE Products SET status = 'R' WHERE id = " + productId + ";")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusOK)
}

func reviewGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	var reviews []struct {
		Name      string `json:"name"`
		Text      string `json:"text"`
		Rating    string `json:"rating"`
		Timestamp string `json:"timestamp"`
	}

	id, hasId := c.Params.Get("id")
	if !hasId {
		c.Status(http.StatusBadRequest)
		return
	}

	rows, err := db.Query("SELECT Reviews.review AS text, Users.name AS name, Reviews.created AS timestamp," +
		" Reviews.rating AS rating FROM Reviews JOIN Users ON Users.id = Reviews.user_id WHERE Reviews.product_id" +
		" = " + id + " ORDER BY Reviews.created DESC;")

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	for rows.Next() {
		var review struct {
			Name      string `json:"name"`
			Text      string `json:"text"`
			Rating    string `json:"rating"`
			Timestamp string `json:"timestamp"`
		}
		if err := rows.Scan(&review.Text, &review.Name, &review.Timestamp, &review.Rating); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		reviews = append(reviews, review)
	}
	c.IndentedJSON(http.StatusOK, gin.H{"reviews": reviews})
}

func reviewPost(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	uid, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var review struct {
		Product string `json:"product" binding:"required"`
		Text    string `json:"text" binding:"required"`
		Rating  string `json:"rating" binding:"required,number"`
	}
	if err := c.BindJSON(&review); err != nil {
		return
	}

	if rating, err := strconv.ParseInt(review.Rating, 10, 16); err != nil || rating > 5 || rating < 1 {
		c.Status(http.StatusBadRequest)
		return
	}

	_, err := db.Query("INSERT INTO Reviews(user_id, review, rating, product_id, created) VALUES(" +
		uid.(string) + ", '" + review.Text + "', " + review.Rating + ", " + review.Product + ", NOW());")
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Status(http.StatusCreated)
}

func orderGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	uid, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var orders []struct {
		Name      string `json:"name"`
		Card      string `json:"card"`
		Quantity  string `json:"quantity"`
		Price     string `json:"price"`
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
	}

	rows, err := db.Query("SELECT Products.name, Cards.number, Orders.quantity, Products.price, Orders.status, Orders.created" +
		" FROM Users JOIN Cards ON Users.id = Cards.user_id JOIN Orders ON Cards.id = Orders.card_id JOIN Products ON " +
		" Products.id = Orders.product_id WHERE Users.id = " + uid.(string) + ";")

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	for rows.Next() {
		var order struct {
			Name      string `json:"name"`
			Card      string `json:"card"`
			Quantity  string `json:"quantity"`
			Price     string `json:"price"`
			Status    string `json:"status"`
			Timestamp string `json:"timestamp"`
		}
		if err := rows.Scan(&order.Name, &order.Card, &order.Quantity, &order.Price, &order.Status, &order.Timestamp); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		orders = append(orders, order)
	}
	c.IndentedJSON(http.StatusOK, gin.H{"orders": orders})
}

func orderQueueGet(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	uid, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var orders []struct {
		Buyer     string `json:"buyer"`
		Name      string `json:"name"`
		Card      string `json:"card"`
		Quantity  string `json:"quantity"`
		Price     string `json:"price"`
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
	}

	rows, err := db.Query("SELECT u1.name, Products.name, c0.number, Orders.quantity, Products.price, Orders.status, Orders.created" +
		" FROM Users AS u0 JOIN Cards AS c0 ON u0.id = c0.user_id JOIN Products ON c0.id = Products.card_id JOIN Orders ON Products.id" +
		" = Orders.product_id JOIN Cards AS c1 ON Orders.card_id = c1.id JOIN Users AS u1 ON c1.user_id = u1.id WHERE u0.id = " + uid.(string) + ";")

	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	for rows.Next() {
		var order struct {
			Buyer     string `json:"buyer"`
			Name      string `json:"name"`
			Card      string `json:"card"`
			Quantity  string `json:"quantity"`
			Price     string `json:"price"`
			Status    string `json:"status"`
			Timestamp string `json:"timestamp"`
		}
		if err := rows.Scan(&order.Buyer, &order.Name, &order.Card, &order.Quantity, &order.Price, &order.Status, &order.Timestamp); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		orders = append(orders, order)
	}
	c.IndentedJSON(http.StatusOK, gin.H{"orders": orders})
}

func orderPost(c *gin.Context, db *sql.DB, rdb *redis.Client) {
	id, exists := c.Get("uid")
	if !exists {
		c.Status(http.StatusUnauthorized)
		return
	}

	var order struct {
		Card     string `json:"card" binding:"required,len=12"`
		Code     string `json:"code" binding:"required,len=4"`
		Product  string `json:"product" binding:"required"`
		Quantity string `json:"quantity" binding:"required,number"`
	}
	if err := c.BindJSON(&order); err != nil {
		return
	}
	var cardId string
	var card string
	var code string
	var balance string

	cardErr := db.QueryRow("SELECT id, number, code, balance FROM Cards WHERE user_id = "+id.(string)+
		" AND number = '"+order.Card+"';").Scan(&cardId, &card, &code, &balance)
	if cardErr != nil {
		c.Status(http.StatusNotFound)
	}
	if code != order.Code {
		c.Status(http.StatusUnauthorized)
		return
	}
	var productCard string
	var qProd string
	var price string
	var status string
	productErr := db.QueryRow("SELECT card_id, quantity, price, status FROM Products WHERE id = "+order.Product+";").Scan(&productCard, &qProd, &price, &status)
	if productErr != nil || status != "A" {
		c.Status(http.StatusNotFound)
		return
	}
	qOrder, qOrderErr := strconv.ParseInt(order.Quantity, 10, 16)
	qProduct, qProductErr := strconv.ParseInt(qProd, 10, 16)
	cardBalance, cardBalanceErr := strconv.ParseFloat(balance, 64)
	pPrice, pPriceErr := strconv.ParseFloat(price, 64)
	if qOrderErr != nil || qProductErr != nil || cardBalanceErr != nil || pPriceErr != nil || qProduct < qOrder {
		c.Status(http.StatusBadRequest)
		return
	}
	cost := float64(qOrder) * pPrice
	if cardBalance < cost {
		c.Status(http.StatusPaymentRequired)
		return
	}
	_, err := db.Query("INSERT INTO Orders(card_id, product_id, quantity, status, created) VALUES(" +
		cardId + ", " + order.Product + ", " + order.Quantity + ", 'A', NOW()); UPDATE Cards SET" +
		" balance = balance - " + strconv.FormatFloat(cost, 'f', -1, 64) + " WHERE id = " + cardId + ";" +
		" UPDATE Products SET quantity = quantity - " + order.Quantity + " WHERE id = " + order.Product + ";" +
		" UPDATE Cards SET balance = balance + " + strconv.FormatFloat(cost, 'f', -1, 64) + " WHERE" +
		" id = " + productCard + ";")
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Status(http.StatusCreated)
}
