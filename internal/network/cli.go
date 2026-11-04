package network

import (
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/spf13/cobra"
)

const endpoint = "/api/network"

// Command uses the same HTTP contract as the UI and generated MCP reads.
func Command() *cobra.Command {
	show := cli.Verb("show", "Inspect network equipment")
	show.RunE = nil
	for _, item := range []struct {
		name, short, path string
		args              int
	}{
		{"config", "Show equipment inventory", "/config", 0},
		{"contract", "Show versioned equipment schemas", "/contract", 0},
		{"snapshot", "Read one device", "/snapshot", 1},
		{"capabilities", "Inspect one device's capabilities", "/capabilities", 1},
	} {
		c := &cobra.Command{Use: item.name, Short: item.short, Args: cobra.ExactArgs(item.args)}
		if item.args == 1 {
			c.Use += " <id>"
		}
		c.RunE = func(c *cobra.Command, args []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			path := endpoint + item.path
			if item.args == 1 {
				path = endpoint + "/devices/" + url.PathEscape(args[0]) + item.path
			}
			var result json.RawMessage
			if err := cli.ClientFor(c).Get(c.Context(), path, &result); err != nil {
				return err
			}
			return cli.JSON(c.OutOrStdout(), result)
		}
		show.AddCommand(c)
	}
	var vlan int
	var lookupMAC string
	fdb := &cobra.Command{Use: "fdb <id>", Short: "Locate a known MAC on a device", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		if err := cli.ValidateOutput(c); err != nil {
			return err
		}
		query := url.Values{"mac": {lookupMAC}}
		if c.Flags().Changed("vlan") {
			query.Set("vlan", fmt.Sprint(vlan))
		}
		var result json.RawMessage
		if err := cli.ClientFor(c).Get(c.Context(), endpoint+"/devices/"+url.PathEscape(args[0])+"/fdb?"+query.Encode(), &result); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), result)
	}}
	fdb.Flags().IntVar(&vlan, "vlan", 0, "VLAN ID to search")
	fdb.Flags().StringVar(&lookupMAC, "mac", "", "MAC address to locate")
	show.AddCommand(fdb)
	var target, clientMAC string
	operation := &cobra.Command{Use: "operation <id>", Short: "Preview disconnecting one wireless client", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		if err := cli.ValidateOutput(c); err != nil {
			return err
		}
		var result OperationPlan
		err := cli.ClientFor(c).Post(c.Context(), endpoint+"/devices/"+url.PathEscape(args[0])+"/operations/plan", Operation{Kind: "client.disconnect", Target: target, ClientMAC: clientMAC}, &result)
		if err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), result)
	}}
	operation.Flags().StringVar(&target, "target", "", "Wireless network interface to target")
	operation.Flags().StringVar(&clientMAC, "mac", "", "Wireless client MAC address")
	show.AddCommand(operation)
	add := cli.Verb("add", "Add managed network equipment")
	add.RunE = nil
	var d Device
	device := &cobra.Command{Use: "device <id>", Short: "Connect a device inventory entry to a brand adapter", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, args []string) error {
		if err := cli.ValidateOutput(c); err != nil {
			return err
		}
		d.ID = args[0]
		if err := (Config{Devices: []Device{d}}).Validate(); err != nil {
			return err
		}
		if cli.DryRun(c) {
			return cli.JSON(c.OutOrStdout(), d)
		}
		var result Config
		if err := cli.ClientFor(c).Put(c.Context(), endpoint+"/devices/"+url.PathEscape(d.ID), d, &result); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), result)
	}}
	device.Flags().StringVar(&d.Adapter, "adapter", "", "Device adapter: mercury or openwrt")
	device.Flags().StringVar(&d.Address, "address", "", "HTTP(S) management origin")
	device.Flags().StringVar(&d.Name, "name", "", "Device display name")
	device.Flags().StringVar(&d.Username, "username", "", "Management login username")
	device.Flags().StringVar(&d.PasswordEnv, "password-env", "", "Daemon environment variable containing the management password")
	add.AddCommand(device)
	rm := cli.Verb("rm", "Forget managed network equipment")
	rm.RunE = nil
	rm.AddCommand(&cobra.Command{Use: "device <id>", Short: "Forget a device without changing the remote equipment", Args: cobra.ExactArgs(1), ValidArgsFunction: completeDevice, RunE: func(c *cobra.Command, args []string) error {
		if err := cli.ValidateOutput(c); err != nil {
			return err
		}
		if cli.DryRun(c) {
			return cli.JSON(c.OutOrStdout(), map[string]string{"remove": args[0]})
		}
		var result Config
		if err := cli.ClientFor(c).Do(c.Context(), "DELETE", endpoint+"/devices/"+url.PathEscape(args[0]), nil, &result); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), result)
	}})
	set := cli.Verb("set", "Execute a confirmed equipment operation")
	set.RunE = nil
	var confirmation string
	disconnect := &cobra.Command{Use: "client <id> <target> <mac>", Short: "Disconnect a wireless client using a preview confirmation", Args: cobra.ExactArgs(3), ValidArgsFunction: completeDevice, RunE: func(c *cobra.Command, args []string) error {
		if err := cli.ValidateOutput(c); err != nil {
			return err
		}
		op := Operation{Kind: "client.disconnect", Target: args[1], ClientMAC: args[2]}
		var result json.RawMessage
		path := endpoint + "/devices/" + url.PathEscape(args[0]) + "/operations/"
		var body any = ExecuteRequest{Operation: op, Confirmation: confirmation}
		if cli.DryRun(c) {
			path += "plan"
			body = op
		} else {
			path += "execute"
			if confirmation == "" {
				return fmt.Errorf("preview with olr network show operation, then provide --confirmation")
			}
		}
		if err := cli.ClientFor(c).Post(c.Context(), path, body, &result); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), result)
	}}
	disconnect.Flags().StringVar(&confirmation, "confirmation", "", "One-use confirmation from the operation preview")
	set.AddCommand(disconnect)
	return cli.NewModule(ModuleName, "Network equipment: switches, access points and routers", show, add, rm, set)
}

func completeDevice(c *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var cfg Config
	if err := cli.ClientFor(c).Get(c.Context(), endpoint+"/config", &cfg); err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	out := make([]string, 0, len(cfg.Devices))
	for _, d := range cfg.Devices {
		out = append(out, d.ID)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
