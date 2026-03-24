package cli

import (
	"fmt"
	"text/tabwriter"
)

// RunTokenCreate performs `dbx token create`.
func RunTokenCreate(name, tokenType, scope, ttl, projectID string, printer *Printer) error {
	if name == "" {
		return fmt.Errorf("token name is required")
	}

	// Default type to pat.
	if tokenType == "" {
		tokenType = "pat"
	}

	if tokenType != "pat" && tokenType != "provision" {
		return fmt.Errorf("token type must be 'pat' or 'provision'")
	}

	if tokenType == "provision" && projectID == "" {
		return fmt.Errorf("--project-id is required for provision tokens")
	}

	// Default scope.
	if scope == "" {
		scope = "read:env"
	}

	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	resp, err := client.CreateToken(name, tokenType, scope, ttl, projectID)
	if err != nil {
		return fmt.Errorf("creating token: %w", err)
	}

	if printer.JSON {
		return printer.Data(resp)
	}

	printer.Success("Token created: %s", name)
	printer.Info("Token:      %s", resp.Token)
	printer.Info("ID:         %s", resp.ID)
	if resp.ExpiresAt != "" {
		printer.Info("Expires at: %s", resp.ExpiresAt)
	}
	printer.Info("")
	printer.Info("Save this token — it will not be shown again.")

	return nil
}

// RunTokenList performs `dbx token list`.
func RunTokenList(printer *Printer) error {
	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	tokens, err := client.ListTokens()
	if err != nil {
		return fmt.Errorf("listing tokens: %w", err)
	}

	if printer.JSON {
		return printer.Data(tokens)
	}

	if len(tokens) == 0 {
		printer.Info("No tokens found. Run `dbx token create` to create one.")
		return nil
	}

	w := tabwriter.NewWriter(printer.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tTYPE\tEXPIRES\tLAST USED")
	for _, t := range tokens {
		expires := t.ExpiresAt
		if expires == "" {
			expires = "-"
		}
		lastUsed := t.LastUsed
		if lastUsed == "" {
			lastUsed = "never"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.Name, t.ID, t.Type, expires, lastUsed)
	}
	return w.Flush()
}

// RunTokenRevoke performs `dbx token revoke <name>`.
func RunTokenRevoke(name string, printer *Printer) error {
	if name == "" {
		return fmt.Errorf("token name or ID is required")
	}

	client, err := ResolveAuth(printer)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Find the token by name or ID.
	tokens, err := client.ListTokens()
	if err != nil {
		return fmt.Errorf("listing tokens: %w", err)
	}

	var tokenID string
	for _, t := range tokens {
		if t.Name == name || t.ID == name {
			tokenID = t.ID
			break
		}
	}

	if tokenID == "" {
		return fmt.Errorf("token %q not found", name)
	}

	if err := client.RevokeToken(tokenID); err != nil {
		return fmt.Errorf("revoking token: %w", err)
	}

	printer.Success("Token %q revoked", name)
	return nil
}
