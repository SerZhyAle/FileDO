package main

import (
	"fmt"
	"strings"

	"filedo/helpers"
)

// handleCheckDuplicatesCommand обрабатывает команду проверки дубликатов,
// когда она используется как самостоятельная команда без контекста устройства/папки/сети
func handleCheckDuplicatesCommand(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("недостаточно аргументов для команды. Используйте: cd from list <file_path> [options]")
	}

	cmd := strings.ToLower(args[0])
	if cmd != "cd" && cmd != "check-duplicates" && cmd != "duplicate" {
		return fmt.Errorf("неизвестная команда: %s", args[0])
	}

	// Проверяем, что команда имеет формат "cd from list file.lst [options]"
	if strings.ToLower(args[1]) != "from" || strings.ToLower(args[2]) != "list" {
		return fmt.Errorf("неверный формат команды. Используйте: cd from list <file_path> [options]")
	}

	// Передаем все аргументы после "cd", т.е. "from list file.lst [options]"
	return helpers.CheckDuplicatesFromFile(args[1:])
}

// handleHistoryCommand обрабатывает команду просмотра истории
func handleHistoryCommand(args []string) error {
	// По умолчанию показываем последние 10 команд
	count := 10

	// Если указан аргумент, пытаемся его использовать как количество записей
	if len(args) > 1 {
		// TODO: добавить парсинг аргументов для указания количества записей истории
	}

	// Вызываем функцию показа истории
	ShowLastHistory(count)
	return nil
}
