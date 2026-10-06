package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/itsnotkubrick/3X-UI_KIT/internal/config"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/db"
	"github.com/itsnotkubrick/3X-UI_KIT/internal/server"
)

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "help", "-h", "--help":
			printHelp()
			return
		}
	}

	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("[FATAL] Ошибка открытия базы данных: %v", err)
	}
	defer database.Close()

	if len(os.Args) < 2 || os.Args[1] == "serve" {
		srv := server.New(cfg, database)
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("[FATAL] Ошибка запуска сервера: %v", err)
		}
		return
	}

	command := os.Args[1]
	switch command {
	case "user":
		handleUserCommand(cfg, database, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func handleUserCommand(cfg *config.Config, database *db.DB, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Укажите действие: add, del, toggle, passwd, token, list, get")
		os.Exit(1)
	}

	action := args[0]
	actionArgs := args[1:]

	switch action {
	case "add":
		if len(actionArgs) < 1 {
			die("Укажите имя пользователя: kit-portal user add <username> [--password P] [--sub-id S]")
		}
		var username string
		var flagArgs []string
		if !strings.HasPrefix(actionArgs[0], "-") {
			username = actionArgs[0]
			flagArgs = actionArgs[1:]
		} else {
			flagArgs = actionArgs
		}

		fs := flag.NewFlagSet("add", flag.ExitOnError)
		pwdFlag := fs.String("password", "", "Пароль пользователя")
		subIDFlag := fs.String("sub-id", "", "Идентификатор подписки (subId)")
		_ = fs.Parse(flagArgs)

		if username == "" && len(fs.Args()) > 0 {
			username = fs.Args()[0]
		}
		if username == "" {
			die("Укажите имя пользователя: kit-portal user add <username> [--password P] [--sub-id S]")
		}

		res, err := database.AddUser(username, *subIDFlag, *pwdFlag)
		if err != nil {
			die(err.Error())
		}
		res.PortalURL = cfg.PortalURL
		res.SingboxSubURL = fmt.Sprintf("%s/sub/singbox?token=%s", cfg.PortalURL, res.SubToken)
		printJSON(res)

	case "del":
		if len(actionArgs) < 1 {
			die("Укажите имя пользователя: kit-portal user del <username>")
		}
		username := actionArgs[0]
		if err := database.DeleteUser(username); err != nil {
			die(err.Error())
		}
		printJSON(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Пользователь %s удален", username),
		})

	case "toggle":
		if len(actionArgs) < 2 {
			die("Использование: kit-portal user toggle <username> <1|0>")
		}
		username := actionArgs[0]
		activeVal, err := strconv.Atoi(actionArgs[1])
		if err != nil {
			die("Значение активности должно быть 1 или 0")
		}
		active := activeVal == 1
		if err := database.ToggleUser(username, active); err != nil {
			die(err.Error())
		}
		status := "заблокирован"
		if active {
			status = "активирован"
		}
		printJSON(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Пользователь %s %s", username, status),
		})

	case "passwd":
		if len(actionArgs) < 2 {
			die("Использование: kit-portal user passwd <username> <new_password>")
		}
		username := actionArgs[0]
		password := actionArgs[1]
		if err := database.SetPassword(username, password); err != nil {
			die(err.Error())
		}
		printJSON(map[string]interface{}{
			"success": true,
			"message": fmt.Sprintf("Пароль для %s обновлен", username),
		})

	case "token":
		if len(actionArgs) < 1 {
			die("Использование: kit-portal user token <username>")
		}
		username := actionArgs[0]
		tok, err := database.RotateToken(username)
		if err != nil {
			die(err.Error())
		}
		printJSON(map[string]interface{}{
			"success":         true,
			"sub_token":       tok,
			"singbox_sub_url": fmt.Sprintf("%s/sub/singbox?token=%s", cfg.PortalURL, tok),
		})

	case "list":
		users, err := database.ListUsers()
		if err != nil {
			die(err.Error())
		}
		printJSON(users)

	case "get":
		if len(actionArgs) < 1 {
			die("Использование: kit-portal user get <username>")
		}
		username := actionArgs[0]
		u, err := database.GetUser(username)
		if err != nil {
			die(err.Error())
		}
		u.PasswordHash = ""
		u.Salt = ""
		printJSON(u)

	default:
		die(fmt.Sprintf("Неизвестное действие: %s", action))
	}
}

func printJSON(data interface{}) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(data)
}

func die(msg string) {
	fmt.Fprintf(os.Stderr, "[ERROR] %s\n", msg)
	os.Exit(1)
}

func printHelp() {
	fmt.Println(`kit-portal – Личный кабинет пользователя и сервис подписок

Использование:
  kit-portal [serve]                                        Запуск сервера
  kit-portal user add <username> [--password P] [--sub-id S] Добавление пользователя
  kit-portal user del <username>                            Удаление пользователя
  kit-portal user toggle <username> <1|0>                   Включение/отключение
  kit-portal user passwd <username> <password>              Смена пароля
  kit-portal user token <username>                          Ротация токена подписки
  kit-portal user list                                      Список пользователей
  kit-portal user get <username>                            Информация о пользователе`)
}
