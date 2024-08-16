package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/pelletier/go-toml"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"golang.org/x/oauth2"
)

var addr string
var i int
var accIndx int
var accOption int
var configOption int
var accIndxToDelete int
var proxyAcc string
var config2 config

var newLocalIP string
var newRemoteIP string

// Only for Windows
func clearConsole() {
	cmd := exec.Command("cmd", "/c", "cls") //windows
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func CheckNetIsolation() {
	for {
		out, _ := exec.Command("CheckNetIsolation", "LoopbackExempt", "-s", `-n="microsoft.minecraftuwp_8wekyb3d8bbwe"`).Output()
		if !strings.Contains(string(out), "microsoft.minecraftuwp_8wekyb3d8bbwe") {
			cmd := exec.Command("CheckNetIsolation", "LoopbackExempt", "-a", `-n="microsoft.minecraftuwp_8wekyb3d8bbwe"`)
			cmd.Stdout = os.Stdout
			cmd.Run()
			continue
		} else {
			break
		}
	}
}

// The following program implements a proxy that forwards players from one local address to a remote address.
func main() {
	config2 = readConfig()
	CheckNetIsolation()
	for {
		clearConsole()
		config2 = readConfig() //read config again for new account
		fmt.Print("Choose: \n1: Choose accounts\n2: Change proxy accounts\n3: Change config\n")
		fmt.Print("input: ")
		fmt.Scanln(&i)
		if i == 1 {
			clearConsole()
			if len(config2.Name.AccountName) == 0 {
				fmt.Print("No proxy account found. Please add a proxy account and try again\n")
				continue
			}
			for a := 0; a < len(config2.Name.AccountName); a++ {
				fmt.Printf("[%d]: %s\n", a+1, config2.Name.AccountName[a])
			}
			fmt.Print("\n0: Go Back\n")
			fmt.Print("input: ")
			fmt.Scanln(&accIndx)
			if accIndx == 0 {
				clearConsole()
				continue
			}
			break
		} else if i == 2 {
			for {
				config2 = readConfig() //read config again for new account
				clearConsole()
				fmt.Print("Change proxy accounts \n\n")
				if len(config2.Name.AccountName) == 0 {
					fmt.Print("There is no accounts!\n")
				}
				for a := 0; a < len(config2.Name.AccountName); a++ {
					fmt.Printf("[%d]: %s\n", a+1, config2.Name.AccountName[a])
				}
				fmt.Print("\nOptions: \n1: Add proxy account\n2: Delete proxy account\n\n0: Go Back\n")
				fmt.Print("Input: ")
				fmt.Scanln(&accOption)
				if accOption == 1 {
					fmt.Print("Type account name: ")
					fmt.Scanln(&proxyAcc)
					writeConfig(proxyAcc, false)
					continue
				} else if accOption == 2 {
					fmt.Print("Type account index need to delete: ")
					fmt.Scanln(&accIndxToDelete)
					writeConfig(config2.Name.AccountName[accIndxToDelete-1], true)
					continue
				} else if accOption == 0 {
					break
				}
			}
		} else if i == 3 {
			for {
				config2 = readConfig() //read config again for new ip
				clearConsole()
				fmt.Print("Change config\n")
				fmt.Print("Format: <IP>:<Port>\nIP: the server ip you need to remote/connect\nPort: the port of server you need to remote/connect\n\n")
				fmt.Printf("Remote server: %s\nLocal server: %s\n\n", config2.Connection.RemoteAddress, config2.Connection.LocalAddress)
				fmt.Print("1: Change remote server ip\n2: Change local server ip (recommended keep it 127.0.0.1:19132)\n\n0: Go Back\n")
				fmt.Print("Input: ")
				fmt.Scanln(&configOption)
				if configOption == 1 { //remote ip
					fmt.Print("Input new server ip with format <IP>:<Port>: ")
					fmt.Scanln(&newRemoteIP)
					writeConfig2(newRemoteIP, newLocalIP)
					continue
				} else if configOption == 2 { //local ip
					fmt.Print("Input new local ip with format <IP>:<Port>: ")
					fmt.Scanln(&newLocalIP)
					writeConfig2(newRemoteIP, newLocalIP)
					continue
				} else if configOption == 0 {
					break
				}
			}
		}
	}

	src := tokenSource(config2.Name.AccountName[accIndx-1])

	p, err := minecraft.NewForeignStatusProvider(config2.Connection.RemoteAddress)
	if err != nil {
		panic(err)
	}
	listener, err := minecraft.ListenConfig{
		StatusProvider: p,
	}.Listen("raknet", config2.Connection.LocalAddress)
	if err != nil {
		panic(err)
	}

	addr = config2.Connection.RemoteAddress

	fmt.Printf("Connected to: %s local address: %s under proxy account name: %s\n", config2.Connection.RemoteAddress, config2.Connection.LocalAddress, config2.Name.AccountName[accIndx-1])
	fmt.Printf("Use this ip and port to join server: %s\n", config2.Connection.LocalAddress)

	defer listener.Close()
	for {
		c, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		go handleConn(c.(*minecraft.Conn), listener, config2, src)
	}
}

// handleConn handles a new incoming minecraft.Conn from the minecraft.Listener passed.
func handleConn(conn *minecraft.Conn, listener *minecraft.Listener, config config, src oauth2.TokenSource) {
	serverConn, err := minecraft.Dialer{
		TokenSource: src,
		ClientData:  conn.ClientData(),
	}.Dial("raknet", addr)
	if err != nil {
		panic(err)
	}
	var g sync.WaitGroup
	g.Add(2)
	go func() {
		if err := conn.StartGame(serverConn.GameData()); err != nil {
			panic(err)
		}
		g.Done()
	}()
	go func() {
		if err := serverConn.DoSpawn(); err != nil {
			panic(err)
		}
		g.Done()
	}()
	g.Wait()

	go func() {
		defer listener.Disconnect(conn, "connection lost")
		defer serverConn.Close()
		for {
			pk, err := conn.ReadPacket()
			if err != nil {
				return
			}

			if err := serverConn.WritePacket(pk); err != nil {
				if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
					_ = listener.Disconnect(conn, disconnect.Error())
				}
				return
			}
		}
	}()
	go func() {
		defer serverConn.Close()
		defer listener.Disconnect(conn, "connection lost")
		for {
			pk, err := serverConn.ReadPacket()
			if pk, ok := pk.(*packet.Transfer); ok {
				addr = fmt.Sprintf("%s:%d", pk.Address, pk.Port)

				pk.Address = "127.0.0.1"
				pk.Port = 19132
			}
			if err != nil {
				if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
					_ = listener.Disconnect(conn, disconnect.Error())
				}
				return
			}
			if err := conn.WritePacket(pk); err != nil {
				return
			}
		}
	}()
}

type config struct {
	Connection struct {
		LocalAddress  string
		RemoteAddress string
	}
	Name struct {
		AccountName []string
	}
}

func readConfig() config {
	c := config{}
	if _, err := os.Stat("config.toml"); os.IsNotExist(err) {
		f, err := os.Create("config.toml")
		if err != nil {
			log.Fatalf("error creating config: %v", err)
		}
		data, err := toml.Marshal(c)
		if err != nil {
			log.Fatalf("error encoding default config: %v", err)
		}
		if _, err := f.Write(data); err != nil {
			log.Fatalf("error writing encoded default config: %v", err)
		}
		_ = f.Close()
	}
	data, err := os.ReadFile("config.toml")
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		log.Fatalf("error decoding config: %v", err)
	}
	if c.Connection.LocalAddress == "" {
		c.Connection.LocalAddress = "127.0.0.1:19132"
	}
	data, _ = toml.Marshal(c)
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		log.Fatalf("error writing config file: %v", err)
	}
	return c
}

func writeConfig(name string, isDelete bool) {
	c := readConfig()
	data, err := os.ReadFile("config.toml")
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		log.Fatalf("error decoding config: %v", err)
	}
	if !isDelete {
		c.Name.AccountName = append(c.Name.AccountName, name)
	} else {
		for i, other := range c.Name.AccountName {
			if other == name {
				c.Name.AccountName = append(c.Name.AccountName[:i], c.Name.AccountName[i+1:]...)
			}
		}
	}
	data, _ = toml.Marshal(c)
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		log.Fatalf("error writing config file: %v", err)
	}
}

func writeConfig2(remoteAddress string, localAddress string) {
	c := readConfig()
	data, err := os.ReadFile("config.toml")
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		log.Fatalf("error decoding config: %v", err)
	}
	if remoteAddress != "" {
		c.Connection.RemoteAddress = remoteAddress
	}
	if localAddress != "" {
		c.Connection.LocalAddress = localAddress
	}
	data, _ = toml.Marshal(c)
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		log.Fatalf("error writing config file: %v", err)
	}
}

// tokenSource returns a token source for using with a gophertunnel client. It either reads it from the
// token.tok file if cached or requests logging in with a device code.
func tokenSource(name string) oauth2.TokenSource {
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	token := new(oauth2.Token)
	tokenData, err := os.ReadFile("token_" + name + ".tok")
	if err == nil {
		_ = json.Unmarshal(tokenData, token)
	} else {
		token, err = auth.RequestLiveToken()
		check(err)
	}
	src := auth.RefreshTokenSource(token)
	_, err = src.Token()
	if err != nil {
		// The cached refresh token expired and can no longer be used to obtain a new token. We require the
		// user to log in again and use that token instead.
		token, err = auth.RequestLiveToken()
		check(err)
		src = auth.RefreshTokenSource(token)
	}
	tok, _ := src.Token()
	b, _ := json.Marshal(tok)
	_ = os.WriteFile("token_"+name+".tok", b, 0644)
	return src
}
