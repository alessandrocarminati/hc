package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"regexp"
	"time"
	"os"
	"errors"
	"log"
)

type Tag struct {
	Regex		string		`json:"Regex"`
	TagStr		string		`json:"TagStr"`
	WPrefix		bool		`json:"WPrefix"`
}

type Item struct {
	Date		time.Time
	SessionID	uint32
	HostName	string
	Command		string
	Tags		map[string]bool
}

type History struct {
	FileBackend	string
	TagsList	[]Tag		`json:"TagsList"`
	ParsedItems	[]Item
	seenCommands	map[string]int
	RegexTagPrefix	string		`json:"RegexPrefix"`
	RawLog		[]string
	LogTree		*LogTree
}

func extractSwitches(commandLine string) []string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", commandLine)
	re := regexp.MustCompile(` (-[^ ="]+)[=" ]`)
	matches := re.FindAllStringSubmatch(commandLine, -1)

	var switches []string
	for _, match := range matches {
		switches = append(switches, match[1])
	}
	return switches
}
func (t Tag)RegexStr(prefix string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", prefix)
	if t.WPrefix {
		return prefix + t.Regex
	}
	return t.Regex
}

func (h *History) ProcessCommand(command string) error{

	debugPrint(log.Printf, levelCrazy, "Args=%s, %t\n", command)
	pattern := `^([0-9]{8}\.[0-9]{6}) - ([0-9a-f]{8}) - (.*)\> (.*)$`
	re := regexp.MustCompile(pattern)

	matches := re.FindStringSubmatch(command)
	if len(matches) != 5 {
		return errors.New(fmt.Sprintf("Invalid command format: %v", command))
	}

	h.RawLog = append(h.RawLog, command)
	dateStr := matches[1]
	sessionIDHex := matches[2]
	hoststr := strings.Split(matches[3], " ")
	hostName := hoststr[0]
	commandStr := matches[4]
	if x , _ := h.seenCommands[commandStr]; x!=1 {
		h.seenCommands[commandStr] = 1
		date, err := time.Parse("20060102.150405", dateStr)
		if err != nil {
			return errors.New(fmt.Sprintf("Error parsing date: %v", err))
		}
		sessionID, err := strconv.ParseUint(sessionIDHex, 16, 32)
		if err != nil {
			return errors.New(fmt.Sprintf("Error parsing session ID: %v", err))
		}

		newItem := Item{
			Date:      date,
			SessionID: uint32(sessionID),
			HostName:  hostName,
			Command:   commandStr,
			Tags:      make(map[string]bool),
		}

		for _, tag := range h.TagsList {
			if m, e := regexp.MatchString(tag.RegexStr(h.RegexTagPrefix), newItem.Command); (m == true) && (e ==nil) {
				newItem.Tags[tag.TagStr] = true
			}
		}
		switches := extractSwitches(commandStr)
		for _, switch_ := range switches{
			newItem.Tags[switch_] = true
		}
		cmds := h.GetCommand(commandStr)
		for _, cmd := range cmds {
			if cmd!="" {
				newItem.Tags[cmd] = true
			}

		}
		h.ParsedItems = append(h.ParsedItems, newItem)

	}
	return nil
}


func (h *History) GetCommand(cmd string) []string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", cmd)
	commands := make([]string, 0)

	separators := []string{"||", "&&", ";"}

	parts := strings.FieldsFunc(cmd, func(r rune) bool {
		for _, sep := range separators {
			if strings.ContainsRune(sep, r) {
				return true
			}
		}
		return false
	})

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			command := h.extractCommand(part)
			if command != "" {
				commands = append(commands, command)
			}
		}
	}

	return commands
}

func (h *History) extractCommand(cmd string) string {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", cmd)
	pattern := h.RegexTagPrefix + `([^ ]+) *`
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(cmd)
	if len(matches) > 0 {
		return matches[len(matches)-1]
	}
	return ""
}

func (h *History) SaveLog(command string) error {
	debugPrint(log.Printf, levelCrazy, "Args=%s\n", command)
	debugPrint(log.Printf, levelCrazy, "SaveLog - open file (%s)\n", h.FileBackend)
	file, err := os.OpenFile(h.FileBackend, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	debugPrint(log.Printf, levelCrazy, "SaveLog - write data '%s'\n", command)
	_, err = file.WriteString(command + "\n")
	if err != nil {
		debugPrint(log.Printf, levelError, "SaveLog - error writing command\n")
		return err
	}

	return nil
}

func NewHistory(TagFile, backendfile string) (*History, error) {
	debugPrint(log.Printf, levelCrazy, "Args=%s, %s\n", TagFile, backendfile)
	h := History{
		ParsedItems: make([]Item, 0),
		seenCommands: map[string]int{},
		FileBackend: backendfile,
		LogTree: &LogTree{Year: make(map[int]*YearNode)},
	}

	file, err := os.Open(TagFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&h)
	if err != nil {
		return nil, err
	}
	return &h, nil
}
