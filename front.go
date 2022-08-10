package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
	"html/template"
	"io"
	"net/http"
	"time"
)

// https://echo.labstack.com/guide/templates/
type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

// Set an error message cookie
func setErrorMessage(c *echo.Context, message string) {
	(*c).SetCookie(&http.Cookie{
		Name:  "errorMessage",
		Value: message,
	})
}

// Read and clear the error message cookie
func lastErrorMessage(c *echo.Context) string {
	cookie, err := (*c).Cookie("errorMessage")
	if err != nil || cookie.Value == "" {
		return ""
	}
	setErrorMessage(c, "")
	return cookie.Value
}

// Authenticate a user using the `browserToken` cookie, and call `f` with a
// reference to the user
func withBrowserAuthentication(app *App, f func(c echo.Context, user *User) error) func(c echo.Context) error {
	return func(c echo.Context) error {
		cookie, err := c.Cookie("browserToken")
		if err != nil || cookie.Value == "" {
			return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
		}

		var user User
		result := app.DB.First(&user, "browser_token = ?", cookie.Value)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				c.SetCookie(&http.Cookie{
					Name: "browserToken",
				})
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			return err
		}

		return f(c, &user)
	}
}

// GET /
func FrontRoot(app *App) func(c echo.Context) error {
	type rootContext struct {
		Config       *Config
		ErrorMessage string
	}

	type profileContext struct {
		Config       *Config
		User         *User
		ErrorMessage string
		SkinURL      *string
		CapeURL      *string
	}

	profile := func(c echo.Context, user *User) error {
		var skinURL *string
		if user.SkinHash.Valid {
			url := SkinURL(app, user.SkinHash.String)
			skinURL = &url
		}

		var capeURL *string
		if user.CapeHash.Valid {
			url := CapeURL(app, user.CapeHash.String)
			capeURL = &url
		}
		return c.Render(http.StatusOK, "profile", profileContext{
			Config:       app.Config,
			User:         user,
			SkinURL:      skinURL,
			CapeURL:      capeURL,
			ErrorMessage: lastErrorMessage(&c),
		})
	}

	return func(c echo.Context) error {
		cookie, err := c.Cookie("browserToken")
		if err != nil || cookie.Value == "" {
			// register/sign in page
			return c.Render(http.StatusOK, "root", rootContext{
				Config:       app.Config,
				ErrorMessage: lastErrorMessage(&c),
			})
		}
		return withBrowserAuthentication(app, profile)(c)
	}
}

// POST /update
func FrontUpdate(app *App) func(c echo.Context) error {
	return withBrowserAuthentication(app, func(c echo.Context, user *User) error {
		playerName := c.FormValue("playerName")
		password := c.FormValue("password")
		preferredLanguage := c.FormValue("preferredLanguage")
		skinModel := c.FormValue("skinModel")
		skinURL := c.FormValue("skinUrl")
		capeURL := c.FormValue("capeUrl")

		if !IsValidPlayerName(playerName) {
			setErrorMessage(&c, "Player name must be between 1 and 16 characters (inclusive).")
			return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
		}
		user.PlayerName = playerName

		if !IsValidPreferredLanguage(preferredLanguage) {
			setErrorMessage(&c, "Invalid preferred language.")
			return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
		}
		user.PreferredLanguage = preferredLanguage

		if password != "" {
			if !IsValidPassword(password) {
				setErrorMessage(&c, "Invalid password.")
			}
			passwordSalt := make([]byte, 16)
			_, err := rand.Read(passwordSalt)
			if err != nil {
				return err
			}
			user.PasswordSalt = passwordSalt

			passwordHash, err := HashPassword(password, passwordSalt)
			if err != nil {
				return err
			}
			user.PasswordHash = passwordHash
		}

		if !IsValidSkinModel(skinModel) {
			return c.NoContent(http.StatusBadRequest)
		}
		user.SkinModel = skinModel

		skinFile, skinFileErr := c.FormFile("skinFile")
		if skinFileErr == nil {
			skinHandle, err := skinFile.Open()
			if err != nil {
				return err
			}
			defer skinHandle.Close()

			validSkinHandle, err := ValidateSkin(app, skinHandle)
			if err != nil {
				setErrorMessage(&c, fmt.Sprintf("Error using that skin: %s", err))
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			err = SetSkin(app, user, validSkinHandle)
			if err != nil {
				return err
			}
		} else if skinURL != "" {
			res, err := http.Get(skinURL)
			if err != nil {
				setErrorMessage(&c, "Couldn't download skin from that URL.")
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			defer res.Body.Close()

			validSkinHandle, err := ValidateSkin(app, res.Body)
			if err != nil {
				setErrorMessage(&c, fmt.Sprintf("Error using that skin: %s", err))
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			err = SetSkin(app, user, validSkinHandle)

			if err != nil {
				return nil
			}
		}

		capeFile, capeFileErr := c.FormFile("capeFile")
		if capeFileErr == nil {
			capeHandle, err := capeFile.Open()
			if err != nil {
				return err
			}
			defer capeHandle.Close()

			validCapeHandle, err := ValidateCape(app, capeHandle)
			if err != nil {
				setErrorMessage(&c, fmt.Sprintf("Error using that cape: %s", err))
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			err = SetCape(app, user, validCapeHandle)
			if err != nil {
				return err
			}
		} else if capeURL != "" {
			res, err := http.Get(capeURL)
			if err != nil {
				setErrorMessage(&c, "Couldn't download cape from that URL.")
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			defer res.Body.Close()

			validCapeHandle, err := ValidateCape(app, res.Body)
			if err != nil {
				setErrorMessage(&c, fmt.Sprintf("Error using that cape: %s", err))
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			err = SetCape(app, user, validCapeHandle)

			if err != nil {
				return nil
			}
		}

		err := app.DB.Save(&user).Error
		if err != nil {
			if IsErrorUniqueFailed(err) {
				setErrorMessage(&c, "That player name is taken.")
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			return err
		}

		return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
	})
}

// POST /logout
func FrontLogout(app *App) func(c echo.Context) error {
	return withBrowserAuthentication(app, func(c echo.Context, user *User) error {
		c.SetCookie(&http.Cookie{
			Name: "browserToken",
		})
		user.BrowserToken = MakeNullString(nil)
		app.DB.Save(user)
		return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
	})
}

// POST /register
func FrontRegister(app *App) func(c echo.Context) error {
	return func(c echo.Context) error {
		username := c.FormValue("username")
		password := c.FormValue("password")

		if username == "" {
			return c.String(http.StatusBadRequest, "Username cannot be blank!")
		}
		if password == "" {
			return c.String(http.StatusBadRequest, "Password cannot be blank!")
		}

		uuid := uuid.New()

		passwordSalt := make([]byte, 16)
		_, err := rand.Read(passwordSalt)
		if err != nil {
			return err
		}

		passwordHash, err := HashPassword(password, passwordSalt)
		if err != nil {
			return err
		}

		browserToken, err := RandomHex(32)
		if err != nil {
			return err
		}

		user := User{
			UUID:              uuid.String(),
			Username:          username,
			PasswordSalt:      passwordSalt,
			PasswordHash:      passwordHash,
			TokenPairs:        []TokenPair{},
			PlayerName:        username,
			PreferredLanguage: "en",
			SkinModel:         SkinModelClassic,
			BrowserToken:      MakeNullString(&browserToken),
		}

		result := app.DB.Create(&user)
		if result.Error != nil {
			if IsErrorUniqueFailed(err) {
				setErrorMessage(&c, "That username is taken.")
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			return result.Error
		}

		c.SetCookie(&http.Cookie{
			Name:    "browserToken",
			Value:   browserToken,
			Expires: time.Now().Add(24 * time.Hour),
		})

		return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
	}
}

// POST /login
func FrontLogin(app *App) func(c echo.Context) error {
	return func(c echo.Context) error {
		username := c.FormValue("username")
		password := c.FormValue("password")

		var user User
		result := app.DB.First(&user, "username = ?", username)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				setErrorMessage(&c, "User not found!")
				return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
			}
			return result.Error
		}

		passwordHash, err := HashPassword(password, user.PasswordSalt)
		if err != nil {
			return err
		}

		if !bytes.Equal(passwordHash, user.PasswordHash) {
			setErrorMessage(&c, "Incorrect password!")
			return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
		}

		browserToken, err := RandomHex(32)
		if err != nil {
			return err
		}

		c.SetCookie(&http.Cookie{
			Name:    "browserToken",
			Value:   browserToken,
			Expires: time.Now().Add(24 * time.Hour),
		})

		user.BrowserToken = MakeNullString(&browserToken)
		app.DB.Save(&user)

		return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
	}
}

// POST /delete-account
func FrontDeleteAccount(app *App) func(c echo.Context) error {
	return withBrowserAuthentication(app, func(c echo.Context, user *User) error {
		c.SetCookie(&http.Cookie{
			Name: "browserToken",
		})

		oldSkinHash := UnmakeNullString(&user.SkinHash)
		oldCapeHash := UnmakeNullString(&user.CapeHash)
		app.DB.Delete(&user)

		if oldSkinHash != nil {
			err := DeleteSkin(app, *oldSkinHash)
			if err != nil {
				return err
			}
		}

		if oldCapeHash != nil {
			err := DeleteCape(app, *oldCapeHash)
			if err != nil {
				return err
			}
		}

		return c.Redirect(http.StatusSeeOther, app.Config.FrontEndServer.URL)
	})
}
