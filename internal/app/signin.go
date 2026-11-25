package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/EchoMusicApp/Echo-Music/internal/cookies"
	"github.com/EchoMusicApp/Echo-Music/internal/m3"
	"github.com/EchoMusicApp/Echo-Music/internal/youtube"
)

type signInState struct {
	open         bool
	err          string
	attempt      uint64
	cookie       string
	browsers     bool
	importFailed string
	try          task
	inWindow     bool
	hostAccess   bool
	advanced     bool
	accounts     []youtube.AccountChannel
	imported     string
	source       string
}

func (s *signInState) reset() {
	s.open, s.err, s.attempt, s.cookie, s.browsers, s.importFailed, s.inWindow, s.hostAccess, s.advanced = false, "", 0, "", false, "", false, false, false
	s.accounts, s.imported, s.source = nil, "", ""
}

func (a *app) openSignIn() {
	acc := &a.account
	if acc.signIn.open {
		return
	}
	if acc.creds.deletePending {
		a.notice = "Wait for sign-out to finish before signing in again."
		return
	}
	acc.signIn.reset()
	acc.signIn.open, acc.signIn.attempt = true, acc.creds.attempt.Add(1)
}

func (a *app) syncSignInDialog() {
	s := &a.account.signIn
	if !s.open && (s.attempt != 0 || s.try.busy() || s.cookie != "") {
		a.dismissSignIn()
	}
}

func (a *app) dismissSignIn() {
	acc := &a.account
	attempt := acc.signIn.attempt
	acc.creds.attempt.Add(1)
	acc.signIn.try.stop()
	acc.signIn.reset()
	if attempt == 0 || acc.creds.store == nil {
		return
	}
	spawn(a, func() error { return acc.creds.deleteForDialog(attempt) }, func(err error) {
		if err != nil {
			a.notice = "The cancelled sign-in could not be removed from storage: " + err.Error()
		}
	})
}

func (a *app) submitSignIn() {
	s := &a.account.signIn
	if s.try.busy() {
		return
	}
	cookie := cookies.Header(s.cookie)
	if _, err := youtube.NewCookieAuth(cookie, youtube.CookieOptions{}); err != nil {
		s.err = "That is not the cookie of a signed-in session: it has no SAPISID. Copy the whole value of the Cookie header."
		return
	}
	a.trySignIn(sessionSource{channel: a.settings.Channel}, signInTimeout, func(context.Context) (string, error) { return cookie, nil })
}

func (a *app) windowSignIn() {
	s := &a.account.signIn
	if s.try.busy() {
		return
	}
	s.inWindow = true
	a.trySignIn(sessionSource{channel: a.settings.Channel}, cookies.SignInTimeout+signInTimeout, a.account.window)
}

func (a *app) importSignIn(browser cookies.Source) {
	s := &a.account.signIn
	if s.try.busy() {
		return
	}
	s.err, s.importFailed, s.hostAccess = "", "", false
	type importedAccounts struct {
		cookie   string
		accounts []youtube.AccountChannel
	}
	startTask(a, &s.try, signInTimeout, func(ctx context.Context) (importedAccounts, error) {
		cookie, err := browser.Read(ctx)
		if err != nil {
			return importedAccounts{}, err
		}
		auth, err := youtube.NewCookieAuth(cookie, youtube.CookieOptions{})
		if err != nil {
			return importedAccounts{}, err
		}
		list, err := a.account.newClient(auth).GetSignInAccounts(ctx)
		if err != nil {
			return importedAccounts{}, err
		}
		if list == nil || len(list.Items) == 0 {
			return importedAccounts{}, errors.New("YouTube did not return any accounts; sign in at music.youtube.com in that browser, then try again")
		}
		return importedAccounts{cookie: cookie, accounts: list.Items}, nil
	}, func(result importedAccounts, err error) {
		if err != nil {
			s.err, s.importFailed = err.Error(), browser.Label
			return
		}
		s.accounts, s.imported, s.source = result.accounts, result.cookie, browser.Label
	})
}

func (a *app) trySignIn(from sessionSource, timeout time.Duration, read func(context.Context) (string, error)) {
	acc := &a.account
	s := &acc.signIn
	s.err, s.importFailed, s.hostAccess = "", "", false
	attempt := s.attempt
	startTask(a, &s.try, timeout, func(ctx context.Context) (*session, error) {
		cookie, err := read(ctx)
		if err != nil {
			return nil, err
		}
		opened, err := acc.openSession(ctx, cookie, from)
		if err != nil {
			return nil, err
		}
		return opened, acc.creds.saveForDialog(ctx, cookie, attempt)
	}, func(opened *session, err error) {
		s.inWindow = false
		if err != nil {
			s.err = err.Error()
			s.importFailed = from.browser
			s.hostAccess = errors.Is(err, cookies.ErrNeedsHostAccess)
			return
		}
		s.reset()
		a.useSession(opened, from)
	})
}

func (a *app) signInDialog(c *ui.Context) {
	s := &a.account.signIn
	if !s.open {
		return
	}
	if s.accounts != nil {
		a.signInAccountPicker(c)
		return
	}
	busy := s.try.busy()
	sc := m3.Of(c).Scheme
	m3.Dialog(c, &s.open, 440, func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(10).Padding(4, 0, 4, 0).Children(func() {
			ui.Box(c).Size(48, 48).Radius(m3.Full).Background(sc.PrimaryContainer).Center().Children(func() {
				ui.Icon(c, m3.IconLogin).FontSize(22).TextColor(sc.OnPrimaryContainer)
			})
			m3.EmphasizedText(c, m3.HeadlineSmall, "Sign in to YouTube Music").TextAlign(ui.Center).TextColor(sc.OnSurface)
			m3.Text(c, m3.BodyMedium, "Sync your library, playlists, and personalized recommendations.").
				TextAlign(ui.Center).TextColor(sc.OnSurfaceVariant)
		})
		ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if m3.Button(c, m3.ButtonSpec{Label: "Sign in with a browser window", Icon: m3.IconLogin, Size: m3.Medium56, Disabled: busy, Key: "sign-in-window"}).FillWidth().Clicked() {
				a.windowSignIn()
			}
			if s.inWindow {
				m3.Text(c, m3.BodySmall, "Finish signing in in the window that opened.").TextAlign(ui.Center).TextColor(sc.Primary)
			}
		})
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			toggle := m3.Button(c, m3.ButtonSpec{Label: "Advanced", Icon: advancedIcon(s.advanced), Kind: m3.TextOnly, Key: "sign-in-advanced"})
			if toggle.Clicked() {
				s.advanced = !s.advanced
			}
		})
		if s.advanced {
			ui.Column(c.Key("sign-in-adv-card")).FillWidth().Padding(14).Gap(10).Radius(m3.Large).
				Background(sc.SurfaceContainerLow).Border(1, sc.OutlineVariant.Alpha(0.25)).Children(func() {
				m3.Text(c, m3.BodySmall, "Import session from a browser signed in at "+musicURL+", or paste your Cookie header:").TextColor(sc.OnSurfaceVariant)
				picker := m3.Button(c, m3.ButtonSpec{Label: "Import from a browser", Icon: m3.IconExpandMore, Kind: m3.Tonal, Size: m3.Small40, Disabled: busy, Key: "import-from"}).FillWidth()
				if picker.Clicked() {
					s.browsers = !s.browsers
				}
				m3.Menu(c, picker, &s.browsers, 240, func() {
					for _, source := range a.account.sources() {
						if m3.MenuItem(c, source.Label, nil).Clicked() {
							s.browsers = false
							a.importSignIn(source)
						}
					}
				})
				if s.importFailed != "" {
					m3.EmphasizedText(c, m3.TitleSmall, "Importing from "+s.importFailed+" did not work. Paste the Cookie header instead.").TextColor(sc.Primary)
				}
				m3.Text(c, m3.BodySmall, "To paste it: open music.youtube.com while signed in, open DevTools (F12) → Network, reload, and copy the Cookie header.").TextColor(sc.OnSurfaceVariant)
				box := ui.Column(c.Key("cookie-box")).Padding(10, 12).Radius(m3.Medium).Background(sc.SurfaceContainerHighest)
				if box.FocusWithin() {
					box.Border(2, sc.Primary)
				}
				box.Children(func() {
					ui.TextAreaBase(c.Key("cookie"), &s.cookie).Placeholder("Paste the Cookie header").Label("Cookie header").
						Password().Height(84).FontSize(13).TextColor(sc.OnSurface).AutoFocus()
				})
			})
		}
		m3.Text(c, m3.LabelSmall, "Echo Music saves the session in a file in its data folder that only your user can read. Sign out to remove Echo Music's saved copy.").
			TextColor(sc.OnSurfaceVariant.Alpha(0.75))
		if s.hostAccess {
			m3.Text(c, m3.BodyMedium, "To open your browser, Echo Music needs permission to start programs outside its Flatpak sandbox. Run this command, then restart Echo Music:").TextColor(sc.Error)
			ui.Column(c).Padding(12, 16).Radius(m3.Large).Background(sc.SurfaceContainerHighest).Children(func() {
				m3.Text(c, m3.BodyMedium, cookies.HostAccessCommand).TextColor(sc.OnSurface).Selectable()
			})
			if m3.Button(c, m3.ButtonSpec{Label: "Copy command", Icon: m3.IconCopy, Kind: m3.Tonal, Key: "copy-host-access"}).Clicked() {
				c.WriteClipboard(cookies.HostAccessCommand)
				c.Toast("Command copied")
			}
		} else if s.err != "" {
			m3.Text(c, m3.BodyMedium, s.err).TextColor(sc.Error).MaxLines(4)
		}
		ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(8).Children(func() {
			if busy {
				m3.LoadingIndicator(c, 32, false)
			}
			if m3.Button(c, m3.ButtonSpec{Label: "Cancel", Kind: m3.TextOnly, Key: "cancel-sign-in"}).Clicked() {
				a.dismissSignIn()
			}
			if s.advanced && m3.Button(c, m3.ButtonSpec{Label: "Sign in", Disabled: busy || strings.TrimSpace(s.cookie) == "", Key: "submit-sign-in"}).Clicked() {
				a.submitSignIn()
			}
		})
	})
}

func (a *app) signInAccountPicker(c *ui.Context) {
	s := &a.account.signIn
	busy := s.try.busy()
	sc := m3.Of(c).Scheme
	m3.Dialog(c, &s.open, 520, func() {
		m3.EmphasizedText(c, m3.HeadlineSmall, "Choose your YouTube account")
		m3.Text(c, m3.BodyMedium, "Choose the account or channel to use from "+s.source+". Echo Music remembers your choice.").TextColor(sc.OnSurfaceVariant)
		for i, account := range s.accounts {
			row := ui.ButtonBase(c.Key("sign-in-account-" + strconv.Itoa(i)))
			row.Height(72).PaddingX(16).Gap(12).AlignItems(ui.Center).Radius(m3.Medium).
				Cursor(ui.CursorPointer).Label(account.Name).Disabled(busy || account.Disabled).
				Background(m3.StateFill(sc.SurfaceContainerHigh, sc.OnSurface, row.Hovered(), row.Pressed(), row.FocusVisible()))
			row.Children(func() {
				a.avatar(c, account.Name, account.Thumbnail, 96, 40)
				ui.Column(c).Grow(1).Gap(4).Children(func() {
					m3.Text(c, m3.TitleMedium, account.Name).SingleLine()
					switch {
					case account.Email != "":
						m3.Text(c, m3.BodySmall, account.Email).SingleLine().TextColor(sc.OnSurfaceVariant)
					case account.Handle != "":
						m3.Text(c, m3.BodySmall, account.Handle).SingleLine().TextColor(sc.OnSurfaceVariant)
					case account.Byline != "":
						m3.Text(c, m3.BodySmall, account.Byline).SingleLine().TextColor(sc.OnSurfaceVariant)
					}
				})
			})
			if row.Clicked() {
				cookie := s.imported
				a.trySignIn(sessionSource{channel: account.ChannelID, accountIndex: account.AccountIndex, browser: s.source, email: account.Email},
					signInTimeout, func(context.Context) (string, error) { return cookie, nil })
			}
		}
		if s.err != "" {
			m3.Text(c, m3.BodyMedium, s.err).TextColor(sc.Error).MaxLines(4)
		}
		ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if busy {
				m3.LoadingIndicator(c, 40, false)
			}
			if m3.Button(c, m3.ButtonSpec{Label: "Back", Kind: m3.TextOnly, Disabled: busy, Key: "back-sign-in"}).Clicked() {
				s.accounts, s.imported, s.source, s.err = nil, "", "", ""
			}
			if m3.Button(c, m3.ButtonSpec{Label: "Cancel", Kind: m3.TextOnly, Key: "cancel-sign-in"}).Clicked() {
				a.dismissSignIn()
			}
		})
	})
}

const musicURL = "https://music.youtube.com"
