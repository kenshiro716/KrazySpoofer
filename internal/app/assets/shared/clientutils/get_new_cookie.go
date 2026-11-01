package clientutils

import (
	"errors"
	"fmt"

	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/permissions"
	"github.com/kenshiro716/KrazySpoofer/internal/app/config"
	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/color"
	"github.com/kenshiro716/KrazySpoofer/internal/console"
	"github.com/kenshiro716/KrazySpoofer/internal/files"
)

var cookieFile = config.Get("cookie_file")

func GetNewCookie(ctx *context.Context, r *request.Request, m string) {
	pauseController := ctx.PauseController

	if !pauseController.Pause() {
		pauseController.WaitIfPaused()
		return
	}

	console.ClearScreen()

	client := ctx.Client
	inputErr := errors.New(m)
	for {
		fmt.Print(ctx.Logger.History.String())
		color.Error.Println(inputErr)

		i, err := console.LongInput("ROBLOSECURITY: ")
		console.ClearScreen()
		if err != nil {
			inputErr = err
			continue
		}

		fmt.Println("Authenticating cookie...")
		err = client.SetCookie(i)
		console.ClearScreen()
		if err != nil {
			inputErr = err
			continue
		}

		fmt.Println("Checking if account can edit universe...")
		err = permissions.CanEditUniverse(ctx, r)
		console.ClearScreen()
		if err != nil {
			inputErr = err
			continue
		}

		break
	}

	fmt.Print(ctx.Logger.History.String())

	if err := files.Write(cookieFile, client.Cookie); err != nil {
		ctx.Logger.Error("Failed to save cookie: ", err)
	}

	pauseController.Unpause()
}
