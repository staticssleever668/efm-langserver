package langserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sourcegraph/jsonrpc2"
)

func (h *langHandler) handleTextDocumentCodeAction(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) (result interface{}, err error) {
	if req.Params == nil {
		return nil, &jsonrpc2.Error{Code: jsonrpc2.CodeInvalidParams}
	}

	var params CodeActionParams
	if err := json.Unmarshal(*req.Params, &params); err != nil {
		return nil, err
	}

	return h.codeAction(params.TextDocument.URI, &params)
}

func (h *langHandler) executeCommand(params *ExecuteCommandParams) (interface{}, error) {
	if len(params.Arguments) != 1 {
		return nil, fmt.Errorf("invalid command")
	}

	uri, ok := params.Arguments[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid argument")
	}
	fname, _ := fromURI(DocumentURI(uri))
	if fname != "" {
		fname = filepath.ToSlash(fname)
		if runtime.GOOS == "windows" {
			fname = strings.ToLower(fname)
		}
	}
	if !strings.HasPrefix(params.Command, "efm-langserver.") {
		return nil, fmt.Errorf("invalid command")
	}
	params.Command = params.Command[15:]

	var command *Command
	for i, v := range h.commands {
		if params.Command == v.Command {
			command = &h.commands[i]
			break
		}
	}
	if command == nil {
		return nil, fmt.Errorf("command not found: %v", params.Command)
	}

	var cmd *exec.Cmd
	var args []string
	var output string
	if !strings.HasPrefix(command.Command, ":") {
		if runtime.GOOS == "windows" {
			args = []string{"/c", command.Command}
			for _, v := range command.Arguments {
				arg := fmt.Sprint(v)
				tmp := replaceCommandInputFilename(arg, fname)
				if tmp != arg && fname == "" {
					h.logger.Println("invalid uri")
					return nil, fmt.Errorf("invalid uri: %v", uri)
				}
				arg = tmp
				args = append(args, arg)
			}
			cmd = exec.Command("cmd", args...)
		} else {
			args = []string{"-c", command.Command}
			for _, v := range command.Arguments {
				arg := fmt.Sprint(v)
				tmp := replaceCommandInputFilename(arg, fname)
				if tmp != arg && fname == "" {
					h.logger.Println("invalid uri")
					return nil, fmt.Errorf("invalid uri: %v", uri)
				}
				arg = tmp
				args = append(args, arg)
				args = append(args, arg)
			}
			cmd = exec.Command("sh", args...)
		}
		cmd.Dir = h.rootPath
		cmd.Env = os.Environ()
		b, err := cmd.CombinedOutput()
		if err != nil {
			return nil, err
		}
		if h.loglevel >= 1 {
			h.logger.Println(cmd.String()+":", string(b))
		}
		output = string(b)
	} else {
		if command.Command == ":reload-config" {
			config, err := LoadConfig(h.filename)
			if err != nil {
				return nil, err
			}
			h.commands = config.Commands
			h.configs = config.Languages
		}
		h.logMessage(LogInfo, "Reloaded configuration file")
		output = "OK"
	}

	return output, nil
}

func filterCommands(uri DocumentURI, commands []Command) []Command {
	results := []Command{}
	for _, v := range commands {
		if v.OS != "" {
			found := false
			for _, os := range strings.FieldsFunc(v.OS, func(r rune) bool { return r == ',' }) {
				if strings.TrimSpace(os) == runtime.GOOS {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		results = append(results, Command{
			Title:     v.Title,
			Command:   fmt.Sprintf("efm-langserver.%s", v.Command),
			Arguments: []interface{}{string(uri)},
		})
	}
	return results
}

func (h *langHandler) codeAction(uri DocumentURI, params *CodeActionParams) ([]Command, error) {
	f, ok := h.files[uri]
	if !ok {
		return nil, fmt.Errorf("document not found: %v", uri)
	}

	commands := []Command{}
	commands = append(commands, filterCommands(uri, h.commands)...)

	if cfgs, ok := h.configs[f.LanguageID]; ok {
		for _, cfg := range cfgs {
			commands = append(commands, filterCommands(uri, cfg.Commands)...)
		}
	}
	if cfgs, ok := h.configs[wildcard]; ok {
		for _, cfg := range cfgs {
			commands = append(commands, filterCommands(uri, cfg.Commands)...)
		}
	}
	return commands, nil
}
