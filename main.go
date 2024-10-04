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

		// Если запрос не является веб-сокетом, возвращаем ошибку.
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	// запуск горутины (ОДНОЙ) так как если это будет просто функция она заблокирует основной поток т.к выполняется бесконечно
	go runHub()

	//ендпоинт на подключение
	app.Get("/ws", websocket.New(func(c *websocket.Conn) {

		// фцнкция которая выполнится в конце
		defer func() {
			unregister <- c
			c.Close()
		}()

		register <- c

		for {
			// "считывает" входящие сообщения и записывает данные в 3 переменные
			messageType, message, err := c.ReadMessage()
			// обработчик если err не null т.е пользователь каким-либо образом отключился
			if err != nil {
				// Если в err ошибка неожиданная
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("read error:", err)
				}
				// websocket: close 1001 (going away) т.е пользователь отключился (ошибка нормальная)
				log.Println(err)
				return
			}

			// досюда доходим если сообщение считалось c.ReadMessage()
			if messageType == websocket.TextMessage {
				broadcast <- string(message)

			} else {
				log.Println("webcosket message received of type", messageType)
			}
		}
	}))

	addr := flag.String("addr", ":8080", "http service address")
	flag.Parse()
	// блокирует основной поток, ожидая входящие запросы
	log.Fatal(app.Listen(*addr))
}

/*
 */
func runHub() {

	// бесконченый цикл
	for {

		/*
			Оператор select позволяет ожидать на нескольких каналах.
			В зависимости от того, какой из каналов получает данные, будет выполнен соответствующий блок кода.
		*/
		select {
		// Когда в канал register поступило значение добавляем его в мапу clients с созданием структуры client
		case connection := <-register:
			clients[connection] = &client{}
			log.Println("connection registered")

		// Когда в канал broadcast поступило значение
		case message := <-broadcast:
			log.Println("message received", message)
			// Отправка сообщения всем клиентам
			for connection, c := range clients {

				// горутина с анонимной функцией
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

		// когда в канал unregister поступило новое значение
		case connection := <-unregister:
			// Удаление пользователя из комнаты
			delete(clients, connection)

			log.Println("connection unregistered")
		}
	}
}
