package main

import (
	"io"
	"net"
	"time"
	"fmt"
	"strconv"
	"strings"
	"flag"
)

var bufsiz int = 4 * 1048576
type data struct {
	Str []byte
	Size int
	Keep  bool
}

func main() {

	tagsFile := flag.String("tags", "", "File name for tags")
	historyFile := flag.String("history", "", "File name for history")
	collectorPort := flag.String("collector-port", "12345", "Port number for collector")
	searcherPort := flag.String("searcher-port", "12344", "Port number for searcher")
	flag.Parse()
	if *tagsFile == "" || *historyFile == "" {
		fmt.Println("Usage: program -tags <tags file> -history <history file> -collector-port <collector port> -searcher-port <searcher port>")
		flag.PrintDefaults()
		return
	}

	ch := make(chan data, 100)
	var conn net.Conn
	var err error

	history, err := NewHistory(*tagsFile, *historyFile)
	if err != nil {
		panic(err)
	}
	history.LoadLogFromFile()
	go cwdata(history, ch)
	go searcher(history, ":" + *searcherPort)
	ln, err := net.Listen("tcp", ":" + *collectorPort)
	if err != nil {
		panic(err)
	}
	for {
		DPrintf(Debug5, "Connection\n")
		conn, err = ln.Accept()
		if err != nil {
			panic( err)
			}
		go receivedata(conn, ch)
		}

	DPrintf(Debug5, "exit\n")
	close(ch)
}

func receivedata(rd net.Conn, ch chan data) {

	defer 	DPrintf(Debug6, "dead\n")
	DPrintf(Debug6, "alive\n")
	b := make([]byte, bufsiz)

	for {
		_ = rd.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

		m, err := rd.Read(b)

		if m == 0 || err == io.EOF {
			break
			}
		DPrintf(Debug7, "sent 2 channel\n")
		ch <- data{Str:b, Size:m, Keep: true}
	}
	DPrintf(Debug7, "Connection closed\n")
	rd.Close()
}

func cwdata(h *History, ch chan data) {

	keep:=true
	defer 	DPrintf(Debug6, "dead\n")
	DPrintf(Debug6, "alive\n")

	for keep==true {
		DPrintf(Debug7, "Look Checkpoint1\n")
		b := <- ch
		DPrintf(Debug7, "Look Checkpoint2\n")
		cmd := string(b.Str[0:b.Size])
		h.SaveLog(strings.TrimSuffix(cmd, "\n"))
		h.ProcessCommand(strings.TrimSuffix(cmd, "\n"))
		DPrintf(Debug7, "Look Checkpoint3\n")
		keep=b.Keep;
	}

}

func searcher(h *History, port string) {

	listener, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return
	}
	defer listener.Close()
	 DPrintf(Debug6, "Server listening on %s\n", port)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("Error accepting connection:", err.Error())
			return
		}

		go handleConnection(conn, h)
	}
}

func handleConnection(conn net.Conn, h *History) {
	defer conn.Close()

	buf := make([]byte, 1024)

	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
		return
	}

	receivedString := string(buf[:n])
	tokens := parseTokens(receivedString)
	result := do_search(h, tokens[0], strings.TrimSuffix(tokens[1], "\n"))
	_, err = conn.Write([]byte(result))
	if err != nil {
		fmt.Println("Error writing:", err.Error())
		return
	}
}

func parseTokens(s string) []string {
	return strings.Split(s, ":")
}

func do_search(h *History, type_, text string) string {
	ret := ""
	switch type_ {
		case "tag":
		for _, item := range h.ParsedItems {
			if v, ok := item.Tags[text];  v && ok {
				ret = ret + fmt.Sprintf("%s %08x - %s, ==> %s\n", item.Date.Format("2006-01-02 15:04:05"), item.SessionID, item.HostName, item.Command)
			}
		}
		case "text":
		for _, item := range h.ParsedItems {
			if strings.Contains(item.Command, text) {
				ret = ret + fmt.Sprintf("%s %08x - %s, ==> %s\n", item.Date.Format("2006-01-02 15:04:05"), item.SessionID, item.HostName, item.Command)
			}
		}
		case "last":
			n, err := strconv.Atoi(text)
			if err != nil {
				ret = "error\n"
				break
			}
			for i:= len(h.ParsedItems)-n; i<len(h.ParsedItems); i++ {
				ret = ret + fmt.Sprintf("%s %08x - %s, ==> %s\n", h.ParsedItems[i].Date.Format("2006-01-02 15:04:05"), h.ParsedItems[i].SessionID, h.ParsedItems[i].HostName, h.ParsedItems[i].Command)
			}
		case "debug":
			n, err := strconv.Atoi(text)
			if err != nil {
				ret = "error\n"
				break
			}
			switch n {
				case 0: DebugLevel = DebugNone
				case 1: DebugLevel = Debug1
				case 2: DebugLevel = Debug2
				case 3: DebugLevel = Debug3
				case 4: DebugLevel = Debug4
				case 5: DebugLevel = Debug5
				case 6: DebugLevel = Debug6
				case 7: DebugLevel = Debug7
				default: ret = "error\n"
			}
			ret = "Done\n"
		default:
			ret = "unsupported\n"
	}
	return ret
}
