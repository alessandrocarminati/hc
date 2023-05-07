package main

import (
	"io"
	"net"
	"os"
	"time"
)

var bufsiz int = 4 * 1048576
type data struct {
	Str []byte
	Size int
	Keep  bool
}

func main() {

	ch := make(chan data, 100)
	var conn net.Conn
	var err error

	go cwdata(os.Stdout, ch)
	ln, err := net.Listen("tcp", ":12345")
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
//		conn.Close()
		}

	DPrintf(Debug5, "exit\n")
	close(ch)
}

func receivedata(rd net.Conn, ch chan data) {

	defer 	DPrintf(Debug6, "dead\n")
	DPrintf(Debug6, "alive\n")
	b := make([]byte, bufsiz)

	for {
		_ = rd.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

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

func cwdata(wr io.Writer, ch chan data) {

	keep:=true
	defer 	DPrintf(Debug6, "dead\n")
	DPrintf(Debug6, "alive\n")

	for keep==true {
		DPrintf(Debug7, "Look Checkpoint1\n")
		b := <- ch
		DPrintf(Debug7, "Look Checkpoint2\n")
		_, err := wr.Write(b.Str[0:b.Size])
		DPrintf(Debug7, "Look Checkpoint3\n")
		if err != nil {
			panic(err)
			}
		keep=b.Keep;
	}

}
