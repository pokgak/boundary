package accountscmd

import (
	"fmt"
	"net/http"

	"github.com/hashicorp/boundary/api"
	"github.com/hashicorp/boundary/api/accounts"
	"github.com/hashicorp/boundary/internal/cmd/base"
	"github.com/hashicorp/boundary/internal/cmd/common"
	"github.com/hashicorp/boundary/sdk/strutil"
	"github.com/mitchellh/cli"
	"github.com/posener/complete"
)

func init() {
	for k, v := range extraActionsFlagsMap {
		flagsMap[k] = append(flagsMap[k], v...)
	}
}

var (
	_ cli.Command             = (*Command)(nil)
	_ cli.CommandAutocomplete = (*Command)(nil)
)

type Command struct {
	*base.Command

	Func string

	// Used for delete operations
	existed bool
	// Used in some output
	plural string

	extraCmdVars
}

func (c *Command) AutocompleteArgs() complete.Predictor {
	return complete.PredictAnything
}

func (c *Command) AutocompleteFlags() complete.Flags {
	return c.Flags().Completions()
}

func (c *Command) Synopsis() string {
	if extra := c.extraSynopsisFunc(); extra != "" {
		return extra
	}

	return common.SynopsisFunc(c.Func, "account")
}

func (c *Command) Help() string {
	var helpStr string
	helpMap := common.HelpMap("account")

	switch c.Func {

	case "read":
		helpStr = helpMap[c.Func]() + c.Flags().Help()

	case "delete":
		helpStr = helpMap[c.Func]() + c.Flags().Help()

	case "list":
		helpStr = helpMap[c.Func]() + c.Flags().Help()

	default:

		helpStr = c.extraHelpFunc(helpMap)

	}

	// Keep linter from complaining if we don't actually generate code using it
	_ = helpMap
	return helpStr
}

var flagsMap = map[string][]string{

	"read": {"id"},

	"delete": {"id"},

	"list": {"auth-method-id"},
}

func (c *Command) Flags() *base.FlagSets {
	if len(flagsMap[c.Func]) == 0 {
		return c.FlagSet(base.FlagSetNone)
	}

	set := c.FlagSet(base.FlagSetHTTP | base.FlagSetClient | base.FlagSetOutputFormat)
	f := set.NewFlagSet("Command Options")
	common.PopulateCommonFlags(c.Command, f, "account", flagsMap[c.Func])

	c.extraFlagsFunc(set, f)

	return set
}

func (c *Command) Run(args []string) int {
	switch c.Func {
	case "":
		return cli.RunResultHelp

	case "create", "update":
		return cli.RunResultHelp

	}

	c.plural = "account"
	switch c.Func {
	case "list":
		c.plural = "accounts"
	}

	f := c.Flags()

	if err := f.Parse(args); err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	if strutil.StrListContains(flagsMap[c.Func], "id") && c.FlagId == "" {
		c.UI.Error("ID is required but not passed in via -id")
		return 1
	}

	var opts []accounts.Option

	if strutil.StrListContains(flagsMap[c.Func], "auth-method-id") {
		switch c.Func {
		case "list":
			if c.FlagAuthMethodId == "" {
				c.UI.Error("AuthMethod ID must be passed in via -auth-method-id or BOUNDARY_AUTH_METHOD_ID")
				return 1
			}
		}
	}

	client, err := c.Client()
	if err != nil {
		c.UI.Error(fmt.Sprintf("Error creating API client: %s", err.Error()))
		return 2
	}
	accountsClient := accounts.NewClient(client)

	switch c.FlagName {
	case "":
	case "null":
		opts = append(opts, accounts.DefaultName())
	default:
		opts = append(opts, accounts.WithName(c.FlagName))
	}

	switch c.FlagDescription {
	case "":
	case "null":
		opts = append(opts, accounts.DefaultDescription())
	default:
		opts = append(opts, accounts.WithDescription(c.FlagDescription))
	}

	var version uint32
	switch c.Func {

	case "change-password":
		switch c.FlagVersion {
		case 0:
			opts = append(opts, accounts.WithAutomaticVersioning(true))
		default:
			version = uint32(c.FlagVersion)
		}

	case "set-password":
		switch c.FlagVersion {
		case 0:
			opts = append(opts, accounts.WithAutomaticVersioning(true))
		default:
			version = uint32(c.FlagVersion)
		}

	}

	if ret := c.extraFlagHandlingFunc(&opts); ret != 0 {
		return ret
	}

	c.existed = true
	var result api.GenericResult

	var listResult api.GenericListResult

	switch c.Func {

	case "read":
		result, err = accountsClient.Read(c.Context, c.FlagId, opts...)

	case "delete":
		_, err = accountsClient.Delete(c.Context, c.FlagId, opts...)
		if apiErr := api.AsServerError(err); apiErr != nil && apiErr.ResponseStatus() == http.StatusNotFound {
			c.existed = false
			err = nil
		}

	case "list":
		listResult, err = accountsClient.List(c.Context, c.FlagAuthMethodId, opts...)

	}

	result, err = c.executeExtraActions(result, err, accountsClient, version, opts)

	if err != nil {
		if apiErr := api.AsServerError(err); apiErr != nil {
			c.UI.Error(fmt.Sprintf("Error from controller when performing %s on %s: %s", c.Func, c.plural, base.PrintApiError(apiErr)))
			return 1
		}
		c.UI.Error(fmt.Sprintf("Error trying to %s %s: %s", c.Func, c.plural, err.Error()))
		return 2
	}

	switch c.Func {

	case "delete":
		switch base.Format(c.UI) {
		case "json":
			c.UI.Output(fmt.Sprintf("{ \"existed\": %t }", c.existed))

		case "table":
			output := "The delete operation completed successfully"
			switch c.existed {
			case true:
				output += "."
			default:
				output += ", however the resource did not exist at the time."
			}
			c.UI.Output(output)
		}

		return 0

	case "list":
		listedItems := listResult.GetItems().([]*accounts.Account)
		switch base.Format(c.UI) {
		case "json":
			switch {

			case len(listedItems) == 0:
				c.UI.Output("null")

			default:
				b, err := base.JsonFormatter{}.Format(listedItems)
				if err != nil {
					c.UI.Error(fmt.Errorf("Error formatting as JSON: %w", err).Error())
					return 1
				}
				c.UI.Output(string(b))
			}

		case "table":
			c.UI.Output(c.printListTable(listedItems))
		}

		return 0

	}

	item := result.GetItem().(*accounts.Account)
	switch base.Format(c.UI) {
	case "table":
		c.UI.Output(printItemTable(item))

	case "json":
		b, err := base.JsonFormatter{}.Format(item)
		if err != nil {
			c.UI.Error(fmt.Errorf("Error formatting as JSON: %w", err).Error())
			return 1
		}
		c.UI.Output(string(b))
	}

	return 0
}
