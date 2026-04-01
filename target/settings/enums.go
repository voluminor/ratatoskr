// Code generated using '_generate/settings'; DO NOT EDIT.
// Generation time: 2026-04-01T16:43:05Z

package settings

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// // // // // // // // // //

type GoAskFormatEnum uint16

const (
	GoAskFormatText GoAskFormatEnum = iota
	GoAskFormatJson
)

// //

var goAskFormatEnumNames = [...]string{
	"text",
	"json",
}

// //

func (e GoAskFormatEnum) String() string {
	if int(e) < len(goAskFormatEnumNames) {
		return goAskFormatEnumNames[e]
	}
	return fmt.Sprintf("GoAskFormatEnum(%d)", e)
}

// //

func (e GoAskFormatEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *GoAskFormatEnum) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseGoAskFormatEnum(s)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func (e GoAskFormatEnum) MarshalYAML() (interface{}, error) {
	return e.String(), nil
}

func (e *GoAskFormatEnum) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseGoAskFormatEnum(node.Value)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func ParseGoAskFormatEnum(s string) (GoAskFormatEnum, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for i, name := range goAskFormatEnumNames {
		if name == s {
			return GoAskFormatEnum(i), nil
		}
	}
	return 0, fmt.Errorf("invalid GoAskFormatEnum: %q", s)
}

type GoConfExportFormatEnum uint16

const (
	GoConfExportFormatYml GoConfExportFormatEnum = iota
	GoConfExportFormatJson
	GoConfExportFormatConf
)

// //

var goConfExportFormatEnumNames = [...]string{
	"yml",
	"json",
	"conf",
}

// //

func (e GoConfExportFormatEnum) String() string {
	if int(e) < len(goConfExportFormatEnumNames) {
		return goConfExportFormatEnumNames[e]
	}
	return fmt.Sprintf("GoConfExportFormatEnum(%d)", e)
}

// //

func (e GoConfExportFormatEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *GoConfExportFormatEnum) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseGoConfExportFormatEnum(s)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func (e GoConfExportFormatEnum) MarshalYAML() (interface{}, error) {
	return e.String(), nil
}

func (e *GoConfExportFormatEnum) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseGoConfExportFormatEnum(node.Value)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func ParseGoConfExportFormatEnum(s string) (GoConfExportFormatEnum, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for i, name := range goConfExportFormatEnumNames {
		if name == s {
			return GoConfExportFormatEnum(i), nil
		}
	}
	return 0, fmt.Errorf("invalid GoConfExportFormatEnum: %q", s)
}

type GoConfGeneratePresetEnum uint16

const (
	GoConfGeneratePresetBasic GoConfGeneratePresetEnum = iota
	GoConfGeneratePresetMedium
	GoConfGeneratePresetFull
)

// //

var goConfGeneratePresetEnumNames = [...]string{
	"basic",
	"medium",
	"full",
}

// //

func (e GoConfGeneratePresetEnum) String() string {
	if int(e) < len(goConfGeneratePresetEnumNames) {
		return goConfGeneratePresetEnumNames[e]
	}
	return fmt.Sprintf("GoConfGeneratePresetEnum(%d)", e)
}

// //

func (e GoConfGeneratePresetEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *GoConfGeneratePresetEnum) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseGoConfGeneratePresetEnum(s)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func (e GoConfGeneratePresetEnum) MarshalYAML() (interface{}, error) {
	return e.String(), nil
}

func (e *GoConfGeneratePresetEnum) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseGoConfGeneratePresetEnum(node.Value)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func ParseGoConfGeneratePresetEnum(s string) (GoConfGeneratePresetEnum, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for i, name := range goConfGeneratePresetEnumNames {
		if name == s {
			return GoConfGeneratePresetEnum(i), nil
		}
	}
	return 0, fmt.Errorf("invalid GoConfGeneratePresetEnum: %q", s)
}

type GoForwardProtoEnum uint16

const (
	GoForwardProtoTcp GoForwardProtoEnum = iota
	GoForwardProtoUdp
)

// //

var goForwardProtoEnumNames = [...]string{
	"tcp",
	"udp",
}

// //

func (e GoForwardProtoEnum) String() string {
	if int(e) < len(goForwardProtoEnumNames) {
		return goForwardProtoEnumNames[e]
	}
	return fmt.Sprintf("GoForwardProtoEnum(%d)", e)
}

// //

func (e GoForwardProtoEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *GoForwardProtoEnum) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseGoForwardProtoEnum(s)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func (e GoForwardProtoEnum) MarshalYAML() (interface{}, error) {
	return e.String(), nil
}

func (e *GoForwardProtoEnum) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseGoForwardProtoEnum(node.Value)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func ParseGoForwardProtoEnum(s string) (GoForwardProtoEnum, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for i, name := range goForwardProtoEnumNames {
		if name == s {
			return GoForwardProtoEnum(i), nil
		}
	}
	return 0, fmt.Errorf("invalid GoForwardProtoEnum: %q", s)
}

type LogLevelConsoleEnum uint16

const (
	LogLevelConsoleDebug LogLevelConsoleEnum = iota
	LogLevelConsoleInfo
	LogLevelConsoleWarn
	LogLevelConsoleError
	LogLevelConsoleFatal
	LogLevelConsolePanic
	LogLevelConsoleDisabled
)

// //

var logLevelConsoleEnumNames = [...]string{
	"debug",
	"info",
	"warn",
	"error",
	"fatal",
	"panic",
	"disabled",
}

// //

func (e LogLevelConsoleEnum) String() string {
	if int(e) < len(logLevelConsoleEnumNames) {
		return logLevelConsoleEnumNames[e]
	}
	return fmt.Sprintf("LogLevelConsoleEnum(%d)", e)
}

// //

func (e LogLevelConsoleEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *LogLevelConsoleEnum) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseLogLevelConsoleEnum(s)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func (e LogLevelConsoleEnum) MarshalYAML() (interface{}, error) {
	return e.String(), nil
}

func (e *LogLevelConsoleEnum) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseLogLevelConsoleEnum(node.Value)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func ParseLogLevelConsoleEnum(s string) (LogLevelConsoleEnum, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for i, name := range logLevelConsoleEnumNames {
		if name == s {
			return LogLevelConsoleEnum(i), nil
		}
	}
	return 0, fmt.Errorf("invalid LogLevelConsoleEnum: %q", s)
}

type LogOutputEnum uint16

const (
	LogOutputConsole LogOutputEnum = iota
	LogOutputFile
	LogOutputBoth
)

// //

var logOutputEnumNames = [...]string{
	"console",
	"file",
	"both",
}

// //

func (e LogOutputEnum) String() string {
	if int(e) < len(logOutputEnumNames) {
		return logOutputEnumNames[e]
	}
	return fmt.Sprintf("LogOutputEnum(%d)", e)
}

// //

func (e LogOutputEnum) MarshalJSON() ([]byte, error) {
	return json.Marshal(e.String())
}

func (e *LogOutputEnum) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := ParseLogOutputEnum(s)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func (e LogOutputEnum) MarshalYAML() (interface{}, error) {
	return e.String(), nil
}

func (e *LogOutputEnum) UnmarshalYAML(node *yaml.Node) error {
	v, err := ParseLogOutputEnum(node.Value)
	if err != nil {
		return err
	}
	*e = v
	return nil
}

// //

func ParseLogOutputEnum(s string) (LogOutputEnum, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	for i, name := range logOutputEnumNames {
		if name == s {
			return LogOutputEnum(i), nil
		}
	}
	return 0, fmt.Errorf("invalid LogOutputEnum: %q", s)
}
