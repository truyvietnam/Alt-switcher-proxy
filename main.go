package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"switcher/auth"

	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/pelletier/go-toml"
	"github.com/sandertv/gophertunnel/minecraft"
	"golang.org/x/oauth2"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var listLength int

type Login struct {
	label       *widget.Label
	button      *widget.Button
	link        *widget.Hyperlink
	altList     *widget.Select
	altList2    *widget.List
	serverEntry *widget.Entry
	portEntry   *widget.Entry
	altForm     *widget.Form

	serverIpLabel *widget.Label
	portLabel     *widget.Label
	currAltLabel  *widget.Label

	server   *minecraft.Conn
	listener *minecraft.Listener
	client   *minecraft.Conn

	authCode string
	currAlt  string
	clicked  bool
	oldList  []string
}

type diagonal struct {
}

func (d diagonal) MinSize(objects []fyne.CanvasObject) fyne.Size {
	w, h := float32(0), float32(0)
	for _, o := range objects {
		childSize := o.MinSize()

		w += childSize.Width
		h += childSize.Height
	}
	if listLength > 5 {
		listLength = 5
	}
	newHeight := h * float32(listLength)
	return fyne.NewSize(w, newHeight)
}

func (d diagonal) Layout(objects []fyne.CanvasObject, containerSize fyne.Size) {
	pos := fyne.NewPos(0, containerSize.Height-d.MinSize(objects).Height)
	for _, o := range objects {
		size := o.MinSize()
		o.Resize(fyne.NewSize(containerSize.Width, size.Height*5))
		o.Move(pos)

		pos = pos.Add(fyne.NewPos(size.Width, size.Height))
	}
}

func newDiagonal() diagonal {
	return diagonal{}
}

// https://gist.github.com/NaniteFactory/0bd94e84bbe939cda7201374a0c261fd
// MessageBox of Win32 API.
func MessageBox(hwnd uintptr, caption, title string, flags uint) int {
	ret, _, _ := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(caption))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		uintptr(flags))

	return int(ret)
}

// MessageBoxPlain of Win32 API.
func MessageBoxPlain(title, caption string) int {
	const (
		NULL  = 0
		MB_OK = 0
	)
	return MessageBox(NULL, caption, title, MB_OK)
}

func CheckNetIsolation() {
	out, _ := exec.Command("CheckNetIsolation", "LoopbackExempt", "-s", `-n="microsoft.minecraftuwp_8wekyb3d8bbwe"`).Output()
	if !strings.Contains(string(out), "microsoft.minecraftuwp_8wekyb3d8bbwe") {
		cmd := exec.Command("CheckNetIsolation", "LoopbackExempt", "-a", `-n="microsoft.minecraftuwp_8wekyb3d8bbwe"`)
		//cmd.Stdout = os.Stdout
		cmd.Run()
	}
}

func NewLogin() *Login {
	return &Login{}
}

func parseURL(urlStr string) *url.URL {
	link, err := url.Parse(urlStr)
	if err != nil {
		fyne.LogError("Could not parse URL", err)
	}

	return link
}

func (l *Login) terminate() {
	if l.server != nil {
		l.server.Close()
		l.listener.Disconnect(l.client, "Closed proxy")
	}
	if l.client != nil {
		l.client.Close()
	}
	if l.listener != nil {
		_ = l.listener.Close()
		l.listener = nil
		l.link.Hide()
		l.label.SetText("Stopped proxy")
		time.Sleep(5 * time.Second)
		l.label.SetText("") //lmao
	}
}

func (l *Login) updateInfoLabel() {
	c := readConfig()
	remoteAddressSplitted := strings.Split(c.Connection.RemoteAddress, ":")
	if remoteAddressSplitted[0] == "" {
		remoteAddressSplitted[0] = "None"
	}

	l.serverIpLabel.SetText("Server IP: " + remoteAddressSplitted[0])
	l.portLabel.SetText("Port: " + remoteAddressSplitted[1])
}

func (l *Login) updateCurrAlt(s string) {
	c := readConfig()
	l.currAlt = s
	c.Alt.CurrentAlt = s
	l.currAltLabel.SetText("Current Alt: " + l.currAlt)
	writeConfig(c)
}

func (l *Login) newConnectionTab() *fyne.Container {
	l.serverIpLabel = widget.NewLabel("")
	l.portLabel = widget.NewLabel("")
	l.currAltLabel = widget.NewLabel("Current Alt: " + l.currAlt)
	l.updateInfoLabel()

	l.label = widget.NewLabel("")

	l.link = widget.NewHyperlink("Open URL", parseURL(""))
	l.link.Hide()

	l.button = widget.NewButton("Start", func() {
		c := readConfig()
		remoteAddressSplitted := strings.Split(c.Connection.RemoteAddress, ":")
		if remoteAddressSplitted[0] == "" {
			MessageBoxPlain("Warning", "You haven't set server IP. Please change it")
			return
		}
		if !l.clicked {
			l.button.Disable()
			go l.run()
		} else {
			l.clicked = false
			go l.terminate()
			l.button.SetText("Start")
			l.link.SetText("Open URL")
		}
	})

	return container.NewVBox(
		widget.NewLabelWithStyle("Connect", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(
			l.serverIpLabel,
			l.portLabel,
		),
		l.currAltLabel,
		l.label,
		l.link,
		container.NewHBox(
			layout.NewSpacer(),
			l.button,
		),
	)
}

func (l *Login) newSettingTab() *fyne.Container {
	c := readConfig()
	listLength = len(c.Alt.AccountName)
	remoteAddressSplitted := strings.Split(c.Connection.RemoteAddress, ":")

	l.serverEntry = widget.NewEntry()
	//l.serverEntry.SetPlaceHolder("Server IP")

	l.portEntry = widget.NewEntry()
	//l.portEntry.SetPlaceHolder("Port: 19132")
	l.portEntry.SetPlaceHolder("19132")

	l.serverEntry.SetText(remoteAddressSplitted[0])
	l.portEntry.SetText(remoteAddressSplitted[1])

	l.serverEntry.OnChanged = func(s string) {
		if l.portEntry.Text == "" {
			c.Connection.RemoteAddress = fmt.Sprintf("%s:%s", s, "19132")
			writeConfig(c)
		} else {
			c.Connection.RemoteAddress = fmt.Sprintf("%s:%s", s, l.portEntry.Text)
			writeConfig(c)
		}
		l.updateInfoLabel()
	}

	l.portEntry.OnChanged = func(s string) {
		if s == "" {
			c.Connection.RemoteAddress = fmt.Sprintf("%s:%s", l.serverEntry.Text, "19132")
			writeConfig(c)
		} else {
			c.Connection.RemoteAddress = fmt.Sprintf("%s:%s", l.serverEntry.Text, s)
			writeConfig(c)
		}
		l.updateInfoLabel()
	}

	return container.NewVBox(
		widget.NewLabelWithStyle("Setting", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewGridWithColumns(2,
			widget.NewLabel("Server IP:"),
			l.serverEntry,
		),
		container.NewGridWithColumns(2,
			widget.NewLabel("Port:"),
			l.portEntry,
		),
	)
}

func (l *Login) newProfileTab() *fyne.Container {
	var toggle bool = false
	var toolbarButton, changeButton *widget.Toolbar
	c := readConfig()

	altEntry := widget.NewEntry()

	l.altForm = widget.NewForm(
		widget.NewFormItem(
			"Alt Name:",
			altEntry,
		),
	)

	l.oldList = c.Alt.AccountName
	altdata := binding.BindStringList(&c.Alt.AccountName)

	l.altForm.Hide()
	l.altForm.OnSubmit = func() {
		c := readConfig()
		c.Alt.AccountName = append(c.Alt.AccountName, altEntry.Text)
		altdata.Append(altEntry.Text)
		l.altForm.Hide()
		altEntry.SetText("")
		l.updateAltList(c.Alt.AccountName)

		toggle = false
	}

	l.altList = widget.NewSelect(c.Alt.AccountName, func(s string) {
		l.updateCurrAlt(s)
	})

	l.altList.SetSelected(c.Alt.CurrentAlt)

	l.altList2 = widget.NewListWithData(altdata,
		func() fyne.CanvasObject {
			return container.NewBorder(
				nil,
				nil,
				nil,
				widget.NewButtonWithIcon("", theme.DeleteIcon(), nil),
				widget.NewLabel(""))
		},
		func(item binding.DataItem, obj fyne.CanvasObject) {
			f := item.(binding.String)
			text := obj.(*fyne.Container).Objects[0].(*widget.Label)
			text.Bind(f)

			btn := obj.(*fyne.Container).Objects[1].(*widget.Button)
			btn.OnTapped = func() {
				val, _ := f.Get()

				altdata.Remove(val)

				c.Alt.AccountName, _ = altdata.Get()
				l.updateAltList(c.Alt.AccountName)
			}
		},
	)

	altListContainer := container.New(
		newDiagonal(),
		l.altList2,
	)
	altListContainer.Hide()

	hideOption := func() {
		changeButton.Show()
		l.altForm.Hide()
		toolbarButton.Hide()
		l.altList.Show()
		altListContainer.Hide()
	}

	showOption := func() {
		changeButton.Hide()
		toolbarButton.Show()
		l.altList.Hide()
		altListContainer.Show()
	}

	//l.altList2.Resize(fyne.NewSize(200, 200))

	toolbarButton = widget.NewToolbar(
		widget.NewToolbarAction(theme.ContentAddIcon(), func() {
			toggle = !toggle
			if toggle {
				l.altForm.Show()
			} else {
				l.altForm.Hide()
				altEntry.SetText("")
			}
		}),
		widget.NewToolbarAction(theme.ConfirmIcon(), func() {
			hideOption()
		}),
		widget.NewToolbarAction(theme.CancelIcon(), func() {
			altdata.Set(l.oldList)
			l.updateAltList(l.oldList)
			updateListLen(l.oldList)
			hideOption()
		}),
	)
	toolbarButton.Hide()

	changeButton = widget.NewToolbar(widget.NewToolbarAction(theme.DocumentCreateIcon(), func() {
		c := readConfig()
		l.oldList = c.Alt.AccountName
		showOption()
	}))

	return container.NewVBox(
		container.NewHBox(
			widget.NewLabelWithStyle("Profile", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
			layout.NewSpacer(),
			changeButton,
			toolbarButton,
		),
		l.altForm,
		l.altList,
		altListContainer,
	)
}

func (l *Login) run() {
	config := readConfig()
	//ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	src := l.tokenSource2(l.currAlt)
	//if l.listener != nil {
	//	go l.terminate()
	//}
	//defer cancel()
	//s, _ := auth.GetXboxNameTag(ctx, src)
	//fmt.Print("return" + s)

	p, err := minecraft.NewForeignStatusProvider(config.Connection.RemoteAddress)
	if err != nil {
		//panic(err)
		MessageBoxPlain("Error", err.Error())
		l.button.Enable()
		return
	}
	l.listener, err = minecraft.ListenConfig{
		StatusProvider: p,
	}.Listen("raknet", config.Connection.LocalAddress)
	if err != nil {
		return
	}

	l.label.SetText("Done Auth")
	l.button.Enable()
	l.button.SetText("Stop")

	l.link.Show()
	l.link.SetText("Add proxy server to MC")

	// https://github.com/phasephasephase/MCBEProtocolURIs
	localAddressSplitted := strings.Split(config.Connection.LocalAddress, ":")
	l.link.SetURL(parseURL(fmt.Sprintf("minecraft://?addExternalServer=%s|%s:%s", "Proxy Server", localAddressSplitted[0], localAddressSplitted[1])))

	l.clicked = true
	//defer l.listener.Close()
	go func() {
		for {
			c, err := l.listener.Accept()
			if err != nil {
				break
			}
			l.client = c.(*minecraft.Conn)
			go l.handleConn(config, src)
		}
	}()
}

func (l *Login) updateAltList(s []string) {
	c := readConfig()
	c.Alt.AccountName = s
	l.altList.SetOptions(c.Alt.AccountName)
	updateListLen(c.Alt.AccountName)
	writeConfig(c)
}

func updateListLen(s []string) {
	listLength = len(s)
}

func main() {
	a := app.New()
	w := a.NewWindow("Bedrock Proxy Alt Switcher")
	l := NewLogin()
	CheckNetIsolation()

	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Connect", theme.LoginIcon(), l.newConnectionTab()),
		container.NewTabItemWithIcon("Profile", theme.AccountIcon(), l.newProfileTab()),
		container.NewTabItemWithIcon("Setting", theme.SettingsIcon(), l.newSettingTab()),
	)

	w.SetContent(tabs)

	w.Resize(fyne.NewSize(500, 300))
	w.ShowAndRun()

}

// handleConn handles a new incoming minecraft.Conn from the minecraft.Listener passed.
func (l *Login) handleConn(config config, src oauth2.TokenSource) {
	var err error
	l.server, err = minecraft.Dialer{
		TokenSource: src,
		ClientData:  l.client.ClientData(),
	}.Dial("raknet", config.Connection.RemoteAddress)
	if err != nil {
		panic(err)
	}
	var g sync.WaitGroup
	g.Add(2)
	go func() {
		if err := l.client.StartGame(l.server.GameData()); err != nil {
			panic(err)
		}
		g.Done()
	}()
	go func() {
		if err := l.server.DoSpawn(); err != nil {
			panic(err)
		}
		g.Done()
	}()
	g.Wait()

	go func() {
		defer l.listener.Disconnect(l.client, "connection lost")
		defer l.server.Close()
		for {
			pk, err := l.client.ReadPacket()
			if err != nil {
				return
			}

			if err := l.server.WritePacket(pk); err != nil {
				if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
					_ = l.listener.Disconnect(l.client, disconnect.Error())
				}
				return
			}
		}
	}()
	go func() {
		defer l.server.Close()
		defer l.listener.Disconnect(l.client, "connection lost")
		for {
			pk, err := l.server.ReadPacket()
			if err != nil {
				if disconnect, ok := errors.Unwrap(err).(minecraft.DisconnectError); ok {
					_ = l.listener.Disconnect(l.client, disconnect.Error())
				}
				return
			}
			if err := l.client.WritePacket(pk); err != nil {
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
	Alt struct {
		AccountName []string
		CurrentAlt  string
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
	//silly hack
	if c.Connection.RemoteAddress == "" {
		c.Connection.RemoteAddress = ":19132"
	}
	data, _ = toml.Marshal(c)
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		log.Fatalf("error writing config file: %v", err)
	}
	return c
}

func writeConfig(c2 config) {
	c := readConfig()
	data, err := os.ReadFile("config.toml")
	if err != nil {
		log.Fatalf("error reading config: %v", err)
	}
	if err := toml.Unmarshal(data, &c); err != nil {
		log.Fatalf("error decoding config: %v", err)
	}
	data, _ = toml.Marshal(c2)
	if err := os.WriteFile("config.toml", data, 0644); err != nil {
		log.Fatalf("error writing config file: %v", err)
	}
}

// tokenSource returns a token source for using with a gophertunnel client. It either reads it from the
// token.tok file if cached or requests logging in with a device code.
func (l *Login) tokenSource2(s string) oauth2.TokenSource {
	check := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	token := new(oauth2.Token)
	tokenData, err := os.ReadFile("token_" + s + ".tok")
	if err == nil {
		_ = json.Unmarshal(tokenData, token)
	} else {
		tokenC, code, url, err := auth.RequestLiveTokenWithCode()
		if err != nil {
			panic(err)
		}
		_ = url
		l.authCode = code
		l.label.SetText(fmt.Sprintf("Auth at %v using the code %v.", url, code))
		l.link.SetURL(parseURL("https://www.microsoft.com/link?otc=" + code))
		l.link.Show()
		check(err)
		token = <-tokenC
	}
	src := auth.RefreshTokenSource(token)
	_, err = src.Token()
	if err != nil {
		tokenC, code, url, err := auth.RequestLiveTokenWithCode()
		if err != nil {
			panic(err)
		}
		_ = url
		l.authCode = code

		l.label.SetText(code)
		l.link.SetURL(parseURL("https://www.microsoft.com/link?otc=" + code))
		l.link.Show()

		token = <-tokenC
		check(err)
		src = auth.RefreshTokenSource(token)
	}
	tok, _ := src.Token()
	b, _ := json.Marshal(tok)
	_ = os.WriteFile("token_"+s+".tok", b, 0644)
	return src
}
