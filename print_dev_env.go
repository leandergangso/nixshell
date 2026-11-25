package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"strings"
)

type nixVar struct {
	Type  string         `json:"type"`
	Value jsontext.Value `json:"value"`
}

type printDevEnv struct {
	Variables     map[string]nixVar `json:"variables"`
	BashFunctions map[string]string `json:"bashFunctions"`
}

func (d printDevEnv) stringVar(key string) string {
	entry, ok := d.Variables[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(entry.Value, &value); err != nil {
		return ""
	}
	return value
}

func (d printDevEnv) script() string {
	// NOTE: i dont like this whole func at all

	var script strings.Builder

	script.WriteString("unset shellHook\n")
	if _, ok := d.Variables["PATH"]; ok {
		script.WriteString("PATH=${PATH:-}\nnix_saved_PATH=\"$PATH\"\n")
	}
	if _, ok := d.Variables["XDG_DATA_DIRS"]; ok {
		script.WriteString("XDG_DATA_DIRS=${XDG_DATA_DIRS:-}\nnix_saved_XDG_DATA_DIRS=\"$XDG_DATA_DIRS\"\n")
	}

	for _, key := range sortedKeys(d.Variables) {
		entry := d.Variables[key]
		switch entry.Type {
		case "exported", "var":
			value := jsonString(entry.Value)
			if entry.Type == "exported" {
				fmt.Fprintf(&script, "export %s=%s\n", key, shellQuote(value))
			} else {
				fmt.Fprintf(&script, "%s=%s\n", key, shellQuote(value))
			}
		case "array":
			var values []string
			if err := json.Unmarshal(entry.Value, &values); err == nil {
				fmt.Fprintf(&script, "%s=(%s)\n", key, shellQuotes(values))
			}
		case "assoc":
			var values map[string]string
			if err := json.Unmarshal(entry.Value, &values); err == nil {
				fmt.Fprintf(&script, "declare -A %s=(%s)\n", key, shellAssoc(values))
			}
		}
	}

	for _, name := range sortedKeys(d.BashFunctions) {
		fmt.Fprintf(&script, "%s() {\n%s\n}\n", name, d.BashFunctions[name])
	}

	if _, ok := d.Variables["PATH"]; ok {
		script.WriteString("PATH=\"$PATH${nix_saved_PATH:+:$nix_saved_PATH}\"\nexport PATH\n")
	}
	if _, ok := d.Variables["XDG_DATA_DIRS"]; ok {
		script.WriteString("XDG_DATA_DIRS=\"$XDG_DATA_DIRS${nix_saved_XDG_DATA_DIRS:+:$nix_saved_XDG_DATA_DIRS}\"\nexport XDG_DATA_DIRS\n")
	}

	script.WriteString("eval ")
	script.WriteString(shellQuote(d.stringVar("shellHook")))
	script.WriteString(" >&2\n")

	return script.String()
}
