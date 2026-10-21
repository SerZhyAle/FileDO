package main

import (
	"fmt"
	"strings"

	"filedo/fileduplicates"
	"filedo/helpers"
)

// handleCheckDuplicatesCommand handles `cd from list <file> [options]`.
//
// Every way this can fail returns an error - a missing list, a list with no
// valid group, a refused or failed removal, a usage mistake - and never just
// prints one, so the caller's reportRunError turns it into exit 2 and a
// `result` event (DUP-11). The words are compared case-insensitively here and
// again in helpers, so `cd FROM LIST` is the same command.
func handleCheckDuplicatesCommand(args []string) error {
	if len(args) < 4 {
		return &fileduplicates.UsageError{Msg: "not enough arguments for the command. Usage: cd from list <file_path> [options]"}
	}

	cmd := strings.ToLower(args[0])
	if cmd != "cd" && cmd != "check-duplicates" && cmd != "duplicate" {
		return &fileduplicates.UsageError{Msg: fmt.Sprintf("unknown command: %s", args[0])}
	}

	// The command must read "cd from list file.lst [options]"
	if !strings.EqualFold(args[1], "from") || !strings.EqualFold(args[2], "list") {
		return &fileduplicates.UsageError{Msg: "invalid command format. Usage: cd from list <file_path> [options]"}
	}

	// Everything after "cd": "from list file.lst [options]"
	return helpers.CheckDuplicatesFromFile(args[1:], configureDuplicateRun)
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
