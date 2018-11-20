package cmd

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fatih/color"
	rg "github.com/gobuffalo/buffalo/generators/refresh"
	"github.com/gobuffalo/events"
	"github.com/gobuffalo/meta"
	"github.com/markbates/refresh/refresh"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func init() {
	events.NamedListen("buffalo:dev", func(e events.Event) {
		if strings.HasPrefix(e.Kind, "refresh:") {
			e.Kind = strings.Replace(e.Kind, "refresh:", "buffalo:dev:", 1)
			events.Emit(e)
		}
	})
}

var devOptions = struct {
	Debug bool
}{}

// devCmd represents the dev command
var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Run the Buffalo app in 'development' mode",
	Long: `Run the Buffalo app in 'development' mode.
This includes rebuilding the application when files change.
This behavior can be changed in .buffalo.dev.yml file.`,
	RunE: func(c *cobra.Command, args []string) error {
		if runtime.GOOS == "windows" {
			color.NoColor = true
		}
		defer func() {
			msg := "There was a problem starting the dev server, Please review the troubleshooting docs: %s\n"
			cause := "Unknown"
			if r := recover(); r != nil {
				if err, ok := r.(error); ok {
					cause = err.Error()
				}
			}
			logrus.Errorf(msg, cause)
		}()
		os.Setenv("GO_ENV", "development")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		wg, ctx := errgroup.WithContext(ctx)

		wg.Go(func() error {
			return startDevServer(ctx)
		})

		wg.Go(func() error {
			return runDevScript(ctx)
		})

		err := wg.Wait()
		if err != context.Canceled {
			return errors.WithStack(err)
		}
		return nil
	},
}

type packageJSON struct {
	Scripts map[string]interface{} `json:"scripts"`
}

func hasNodeJsScript(app meta.App, s string) bool {
	if !app.WithNodeJs {
		return false
	}
	b, err := ioutil.ReadFile(filepath.Join(app.Root, "package.json"))
	if err != nil {
		return false
	}
	p := packageJSON{}
	if err := json.Unmarshal(b, &p); err != nil {
		return false
	}
	_, ok := p.Scripts[s]
	return ok
}

func runDevScript(ctx context.Context) error {
	app := meta.New(".")
	if !hasNodeJsScript(app, "dev") {
		// there's no dev script, so don't do anything
		return nil
	}
	tool := "yarnpkg"
	if !app.WithYarn {
		tool = "npm"
	}
	if _, err := exec.LookPath(tool); err != nil {
		return errors.Errorf("couldn't find %s tool", tool)
	}
	cmd := exec.CommandContext(ctx, tool, "run", "dev")
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd.Run()
}

func startDevServer(ctx context.Context) error {
	cfgFile := "./.buffalo.dev.yml"
	if _, err := os.Stat(cfgFile); err != nil {
		err = rg.Run("./", map[string]interface{}{
			"name": "buffalo",
		})
		if err != nil {
			return err
		}
	}
	c := &refresh.Configuration{}
	if err := c.Load(cfgFile); err != nil {
		return err
	}
	c.Debug = devOptions.Debug

	app := meta.New(".")
	bt := app.BuildTags("development")
	if len(bt) > 0 {
		c.BuildFlags = append(c.BuildFlags, "-tags", bt.String())
	}
	r := refresh.NewWithContext(c, ctx)
	return r.Start()
}

func init() {
	devCmd.Flags().BoolVarP(&devOptions.Debug, "debug", "d", false, "use delve to debug the app")
	decorate("dev", devCmd)
	RootCmd.AddCommand(devCmd)
}
