package fonts

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo/ui"
)

//go:embed GoogleSans.ttf
var GoogleSans []byte

const FontFamily = "Google Sans"

var UIFont string

func init() {
	if err := ui.RegisterFont(GoogleSans, FontFamily); err != nil {
		log.Printf("registering %s: %v", FontFamily, err)
		return
	}
	UIFont = FontFamily
}
