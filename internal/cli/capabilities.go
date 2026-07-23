package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/prowl-agent/prowl-agent/internal/capability"
)

func newCapabilitiesCmd() *cobra.Command {
	command := &cobra.Command{Use: "capabilities", Short: "Discover Prowl workflows before loading details"}
	command.AddCommand(newCapabilitiesSearchCmd(nil), newCapabilitiesGetCmd(nil))
	return command
}

func newCapabilitiesSearchCmd(catalog *capability.Catalog) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "search [query]", Args: cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			resolved, err := resolveCapabilityCatalog(catalog)
			if err != nil {
				return err
			}
			query := ""
			if len(args) == 1 {
				query = args[0]
			}
			matches := resolved.Search(query, 10)
			if asJSON {
				return writeCapabilityJSON(command, matches)
			}
			if len(matches) == 0 {
				_, err = fmt.Fprintln(command.OutOrStdout(), "No matching capabilities.")
				return err
			}
			for _, match := range matches {
				if _, err := fmt.Fprintf(command.OutOrStdout(), "%s — %s\n  %s\n", match.Name, match.Title, match.Description); err != nil {
					return err
				}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable JSON")
	return command
}

func newCapabilitiesGetCmd(catalog *capability.Catalog) *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use: "get <name>", Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			resolved, err := resolveCapabilityCatalog(catalog)
			if err != nil {
				return err
			}
			manifest, ok := resolved.Get(args[0])
			if !ok {
				return fmt.Errorf("capability not found: %s", args[0])
			}
			if asJSON {
				return writeCapabilityJSON(command, manifest)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "%s — %s\n%s\n\nTools: %v\nResources: %v\nOutputs: %v\nPrivacy: %s\nRead only: %t\nVersion: %s\n", manifest.Name, manifest.Title, manifest.Description, manifest.Tools, manifest.Resources, manifest.Outputs, manifest.Privacy, manifest.ReadOnly, manifest.Version)
			return err
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable JSON")
	return command
}

func resolveCapabilityCatalog(catalog *capability.Catalog) (*capability.Catalog, error) {
	if catalog != nil {
		return catalog, nil
	}
	return capability.BuiltinCatalog()
}

func writeCapabilityJSON(command *cobra.Command, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(command.OutOrStdout(), string(encoded))
	return err
}
