package main

import (
	"flag"
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"log"
	"sync"
)

type client struct {
	isClosing bool
	mu        sync.Mutex
}

var clients = make(map[*websocket.Conn]*client)
var register = make(chan *websocket.Conn)
var broadcast = make(chan string)
var unregister = make(chan *websocket.Conn)

func main() {
	app := fiber.New()

	// Страница на / будет home.html
	app.Static("/", "./home.html")

	/* Мидлваер с помощью use в контором определена функция с параметром c у которой тип - ссылка на объект fiber.Ctx
	ctx - context - Это структура, предоставляемая библиотекой Fiber, которая содержит всю информацию о текущем HTTP-запросе
	и предоставляет методы для работы с ним.
	*/
	app.Use(func(c *fiber.Ctx) error {

		// функция считывает заголовки http для определенния содержит ли он Upgrade и Connection, которые являются обязательными для веб-сокетов.
		if websocket.IsWebSocketUpgrade(c) {
			//log.Println(c.Request())
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	// запуск горутины (ОДНОЙ)
	go runHub()

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		defer func() {
			unregister <- c
			c.Close()
		}()

		register <- c

		for {
			messageType, message, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("read error:", err)
				}

				return
			}

			if messageType == websocket.TextMessage {
				broadcast <- string(message)

			} else {
				log.Println("webcosket message received of type", messageType)
			}
		}
	}))

	addr := flag.String("addr", ":8080", "http service address")
	flag.Parse()
	log.Fatal(app.Listen(*addr))
}

func runHub() {
	for {
		select {
		case connection := <-register:
			clients[connection] = &client{}
			log.Println("connection registered")

		// Когда в broadcast что-то появлиось
		case message := <-broadcast:
			log.Println("message received", message)
			// Отправка сообщения всем клиентам
			for connection, c := range clients {
				go func(connection *websocket.Conn, c *client) {
					c.mu.Lock()
					defer c.mu.Unlock()
					if c.isClosing {
						return
					}
					if err := connection.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
						c.isClosing = true
						log.Println("write error:", err)

						connection.WriteMessage(websocket.CloseMessage, []byte{})
						connection.Close()
						unregister <- connection
					}
				}(connection, c)
			}

		case connection := <-unregister:
			// Удаление пользователя из комнаты
			delete(clients, connection)

			log.Println("connection unregistered")
		}
	}
}
