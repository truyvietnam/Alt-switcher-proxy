package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"golang.org/x/oauth2"
)

// minecraftAuthURL is the URL that an authentication request is made to to get an encoded JWT claim chain.
const minecraftAuthURL = `https://multiplayer.minecraft.net/authentication`

// RequestMinecraftChain requests a fully processed Minecraft JWT chain using the XSTS token passed, and the
// ECDSA private key of the client. This key will later be used to initialise encryption, and must be saved
// for when packets need to be decrypted/encrypted.
func RequestMinecraftChain(ctx context.Context, token *XBLToken, key *ecdsa.PrivateKey) (string, error) {
	data, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)

	// The body of the requests holds a JSON object with one key in it, the 'identityPublicKey', which holds
	// the public key data of the private key passed.
	body := `{"identityPublicKey":"` + base64.StdEncoding.EncodeToString(data) + `"}`
	request, _ := http.NewRequestWithContext(ctx, "POST", minecraftAuthURL, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	// The Authorization header is important in particular. It is composed of the 'uhs' found in the XSTS
	// token, and the Token it holds itself.
	token.SetAuthHeader(request)
	request.Header.Set("User-Agent", "MCPE/Android")
	request.Header.Set("Client-Version", protocol.CurrentVersion)

	c := &http.Client{}
	resp, err := c.Do(request)
	if err != nil {
		return "", fmt.Errorf("POST %v: %w", minecraftAuthURL, err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("POST %v: %v", minecraftAuthURL, resp.Status)
	}
	data, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	c.CloseIdleConnections()
	return string(data), err
}

func GetXboxNameTag(ctx context.Context, src oauth2.TokenSource) (string, error) {
	//key, _ := ecdsa.GenerateKey(elliptic.P384(), cryptorand.Reader)
	//data, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)

	liveToken, err := src.Token()
	if err != nil {
		return "Nah", fmt.Errorf("request Live Connect token: %w", err)
	}
	xsts, err := RequestXBLToken(ctx, liveToken, "http://xboxlive.com/")
	if err != nil {
		return "Nah", fmt.Errorf("request XBOX Live token: %w", err)
	}
	request, err := http.NewRequest("GET", "https://profile.xboxlive.com/users/me/profile/settings/", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("x-xbl-contract-version", "2")
	xsts.SetAuthHeader(request)

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return "s", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "a", err
	}

	if resp.StatusCode >= 400 {
		return string(body), fmt.Errorf("HTTP Error: %d", resp.StatusCode)
	}

	return string(body), nil

	//return xsts.AuthorizationToken.DisplayClaims.UserInfo[0].UserHash, err
}
