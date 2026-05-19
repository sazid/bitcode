package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type InputRepair struct {
	Path   string
	Detail string
}

func (r InputRepair) String() string {
	if r.Path == "" || r.Path == "$" {
		return r.Detail
	}
	return fmt.Sprintf("%s: %s", r.Path, r.Detail)
}

type toolInputIssue struct {
	Path    string
	Message string
}

type ToolInputError struct {
	ToolName string
	Issues   []toolInputIssue
}

func (e *ToolInputError) Error() string {
	if len(e.Issues) == 0 {
		return fmt.Sprintf("invalid input for %s", e.ToolName)
	}

	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		if issue.Path == "" || issue.Path == "$" {
			parts = append(parts, issue.Message)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", issue.Path, issue.Message))
	}
	return fmt.Sprintf("invalid input for %s: %s", e.ToolName, strings.Join(parts, "; "))
}

func normalizeInputForTool(toolName, input string, schema map[string]any) (string, []InputRepair, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		if schemaType(schema) == "object" && len(requiredFields(schema)) == 0 {
			return "{}", []InputRepair{{Path: "$", Detail: "replaced empty arguments with an empty object"}}, nil
		}
		return "", nil, &ToolInputError{
			ToolName: toolName,
			Issues:   []toolInputIssue{{Path: "$", Message: "arguments must be valid JSON"}},
		}
	}

	value, err := decodeJSONValue(trimmed)
	if err != nil {
		return "", nil, &ToolInputError{
			ToolName: toolName,
			Issues:   []toolInputIssue{{Path: "$", Message: fmt.Sprintf("arguments must be valid JSON: %v", err)}},
		}
	}
	if value == nil && schemaType(schema) == "object" && len(requiredFields(schema)) == 0 {
		return "{}", []InputRepair{{Path: "$", Detail: "replaced null arguments with an empty object"}}, nil
	}

	initialIssues := validateSchemaValue(schema, value, "$")
	var repairs []InputRepair
	repairSchemaValue(schema, &value, "$", false, &repairs)

	if len(repairs) == 0 && len(initialIssues) == 0 {
		return input, nil, nil
	}

	remainingIssues := validateSchemaValue(schema, value, "$")
	if len(remainingIssues) > 0 {
		return "", repairs, &ToolInputError{ToolName: toolName, Issues: remainingIssues}
	}

	repaired, err := json.Marshal(value)
	if err != nil {
		return "", repairs, fmt.Errorf("failed to encode repaired input for %s: %w", toolName, err)
	}
	return string(repaired), repairs, nil
}

func decodeJSONValue(raw string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("multiple JSON values")
	}
	return value, nil
}

func validateSchemaValue(schema map[string]any, value any, path string) []toolInputIssue {
	expectedType := schemaType(schema)
	if expectedType == "" {
		return nil
	}

	if value == nil {
		return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected %s, got null", expectedType)}}
	}

	var issues []toolInputIssue
	switch expectedType {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected object, got %s", valueType(value))}}
		}

		props := schemaProperties(schema)
		for _, name := range requiredFields(schema) {
			childPath := joinJSONPath(path, name)
			if _, ok := obj[name]; !ok {
				issues = append(issues, toolInputIssue{Path: childPath, Message: "required field is missing"})
				continue
			}
			if obj[name] == nil {
				childType := schemaType(props[name])
				if childType == "" {
					childType = "non-null value"
				}
				issues = append(issues, toolInputIssue{Path: childPath, Message: fmt.Sprintf("expected %s, got null", childType)})
			}
		}

		for name, childValue := range obj {
			childSchema, ok := props[name]
			if !ok {
				continue
			}
			if childValue == nil {
				continue
			}
			issues = append(issues, validateSchemaValue(childSchema, childValue, joinJSONPath(path, name))...)
		}

	case "array":
		arr, ok := value.([]any)
		if !ok {
			return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected array, got %s", valueType(value))}}
		}
		itemSchema := schemaItems(schema)
		for i, item := range arr {
			issues = append(issues, validateSchemaValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, i))...)
		}

	case "string":
		s, ok := value.(string)
		if !ok {
			return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected string, got %s", valueType(value))}}
		}
		if minLen, ok := schemaMinLength(schema); ok && len(s) < minLen {
			issues = append(issues, toolInputIssue{Path: path, Message: fmt.Sprintf("must be at least %d characters", minLen)})
		}
		if isPathJSONPath(path) && hasDegenerateMarkdownLink(s) {
			issues = append(issues, toolInputIssue{Path: path, Message: "path contains a markdown auto-link wrapper"})
		}
		issues = append(issues, validateEnum(schema, value, path)...)

	case "integer":
		if !isIntegerValue(value) {
			return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected integer, got %s", valueType(value))}}
		}
		issues = append(issues, validateEnum(schema, value, path)...)

	case "number":
		if !isNumberValue(value) {
			return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected number, got %s", valueType(value))}}
		}
		issues = append(issues, validateEnum(schema, value, path)...)

	case "boolean":
		if _, ok := value.(bool); !ok {
			return []toolInputIssue{{Path: path, Message: fmt.Sprintf("expected boolean, got %s", valueType(value))}}
		}
		issues = append(issues, validateEnum(schema, value, path)...)
	}

	return issues
}

func repairSchemaValue(schema map[string]any, value *any, path string, required bool, repairs *[]InputRepair) {
	expectedType := schemaType(schema)
	if expectedType == "" || value == nil {
		return
	}

	switch expectedType {
	case "object":
		repairObjectValue(schema, value, path, repairs)
	case "array":
		repairArrayValue(schema, value, path, repairs)
	case "string":
		repairStringValue(value, path, repairs)
	case "integer":
		repairIntegerValue(value, path, repairs)
	case "number":
		repairNumberValue(value, path, repairs)
	case "boolean":
		repairBooleanValue(value, path, repairs)
	}
}

func repairObjectValue(schema map[string]any, value *any, path string, repairs *[]InputRepair) {
	if str, ok := (*value).(string); ok {
		if parsed, ok := parseStringifiedValue(str, "object"); ok {
			*value = parsed
			*repairs = append(*repairs, InputRepair{Path: path, Detail: "parsed stringified object"})
		}
	}

	obj, ok := (*value).(map[string]any)
	if !ok {
		return
	}

	props := schemaProperties(schema)
	required := requiredFieldSet(schema)
	repairPropertyAliases(obj, props, path, repairs)

	for name, childSchema := range props {
		childValue, ok := obj[name]
		if !ok {
			continue
		}

		childPath := joinJSONPath(path, name)
		if childValue == nil {
			if !required[name] {
				delete(obj, name)
				*repairs = append(*repairs, InputRepair{Path: childPath, Detail: "omitted null optional field"})
			}
			continue
		}

		repairSchemaValue(childSchema, &childValue, childPath, required[name], repairs)
		obj[name] = childValue
	}
}

func repairArrayValue(schema map[string]any, value *any, path string, repairs *[]InputRepair) {
	switch current := (*value).(type) {
	case string:
		if parsed, ok := parseStringifiedValue(current, "array"); ok {
			*value = parsed
			*repairs = append(*repairs, InputRepair{Path: path, Detail: "parsed stringified array"})
		} else {
			*value = []any{current}
			*repairs = append(*repairs, InputRepair{Path: path, Detail: "wrapped bare string in an array"})
		}
	case map[string]any:
		if len(current) == 0 {
			*value = []any{}
			*repairs = append(*repairs, InputRepair{Path: path, Detail: "replaced empty object placeholder with an empty array"})
		} else {
			*value = []any{current}
			*repairs = append(*repairs, InputRepair{Path: path, Detail: "wrapped object in an array"})
		}
	}

	arr, ok := (*value).([]any)
	if !ok {
		return
	}

	itemSchema := schemaItems(schema)
	for i, item := range arr {
		itemPath := fmt.Sprintf("%s[%d]", path, i)
		repairSchemaValue(itemSchema, &item, itemPath, false, repairs)
		arr[i] = item
	}
}

func repairStringValue(value *any, path string, repairs *[]InputRepair) {
	str, ok := (*value).(string)
	if !ok || !isPathJSONPath(path) {
		return
	}

	unwrapped := unwrapDegenerateMarkdownLinks(str)
	if unwrapped == str {
		return
	}

	*value = unwrapped
	*repairs = append(*repairs, InputRepair{Path: path, Detail: "unwrapped markdown auto-link from path"})
}

func repairIntegerValue(value *any, path string, repairs *[]InputRepair) {
	str, ok := (*value).(string)
	if !ok {
		return
	}

	trimmed := strings.TrimSpace(str)
	n, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil {
		return
	}

	*value = json.Number(strconv.FormatInt(n, 10))
	*repairs = append(*repairs, InputRepair{Path: path, Detail: "parsed numeric string as integer"})
}

func repairNumberValue(value *any, path string, repairs *[]InputRepair) {
	str, ok := (*value).(string)
	if !ok {
		return
	}

	trimmed := strings.TrimSpace(str)
	n, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return
	}

	*value = json.Number(strconv.FormatFloat(n, 'f', -1, 64))
	*repairs = append(*repairs, InputRepair{Path: path, Detail: "parsed numeric string as number"})
}

func repairBooleanValue(value *any, path string, repairs *[]InputRepair) {
	str, ok := (*value).(string)
	if !ok {
		return
	}

	b, err := strconv.ParseBool(strings.TrimSpace(str))
	if err != nil {
		return
	}

	*value = b
	*repairs = append(*repairs, InputRepair{Path: path, Detail: "parsed boolean string as boolean"})
}

func parseStringifiedValue(raw, expectedType string) (any, bool) {
	trimmed := strings.TrimSpace(raw)
	if expectedType == "array" && !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	if expectedType == "object" && !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}

	value, err := decodeJSONValue(trimmed)
	if err != nil {
		return nil, false
	}
	if expectedType == "array" {
		_, ok := value.([]any)
		return value, ok
	}
	if expectedType == "object" {
		_, ok := value.(map[string]any)
		return value, ok
	}
	return nil, false
}

func repairPropertyAliases(obj map[string]any, props map[string]map[string]any, path string, repairs *[]InputRepair) {
	if len(obj) == 0 || len(props) == 0 {
		return
	}

	propNames := make([]string, 0, len(props))
	for name := range props {
		propNames = append(propNames, name)
	}
	sort.Strings(propNames)

	for key, value := range obj {
		if _, known := props[key]; known {
			continue
		}

		for _, propName := range propNames {
			if _, exists := obj[propName]; exists {
				continue
			}
			if !isPropertyAlias(key, propName) {
				continue
			}
			obj[propName] = value
			delete(obj, key)
			*repairs = append(*repairs, InputRepair{
				Path:   joinJSONPath(path, propName),
				Detail: fmt.Sprintf("renamed model argument %q to %q", key, propName),
			})
			break
		}
	}
}

func isPropertyAlias(key, propName string) bool {
	if canonicalPropertyName(key) == canonicalPropertyName(propName) {
		return true
	}

	for _, alias := range explicitPropertyAliases(propName) {
		if canonicalPropertyName(key) == canonicalPropertyName(alias) {
			return true
		}
	}
	return false
}

func explicitPropertyAliases(propName string) []string {
	switch propName {
	case "file_path":
		return []string{"path", "filePath", "filepath", "absolutePath", "absolute_path", "absoluteFilePath", "absolute_file_path"}
	case "old_string":
		return []string{"oldString", "old"}
	case "new_string":
		return []string{"newString", "new"}
	case "replace_all":
		return []string{"replaceAll"}
	case "allowed_domains":
		return []string{"allowedDomains", "domains"}
	case "blocked_domains":
		return []string{"blockedDomains"}
	case "agent_type":
		return []string{"agentType", "type"}
	case "shell":
		return []string{"shellName"}
	case "command":
		return []string{"cmd", "shellCommand", "shell_command"}
	default:
		return nil
	}
}

func canonicalPropertyName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func schemaType(schema map[string]any) string {
	if schema == nil {
		return ""
	}
	if t, ok := schema["type"].(string); ok {
		return t
	}
	if _, ok := schema["properties"]; ok {
		return "object"
	}
	if _, ok := schema["items"]; ok {
		return "array"
	}
	return ""
}

func schemaProperties(schema map[string]any) map[string]map[string]any {
	rawProps, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}

	props := make(map[string]map[string]any, len(rawProps))
	for name, rawSchema := range rawProps {
		if child, ok := rawSchema.(map[string]any); ok {
			props[name] = child
		}
	}
	return props
}

func schemaItems(schema map[string]any) map[string]any {
	if rawItems, ok := schema["items"].(map[string]any); ok {
		return rawItems
	}
	return nil
}

func requiredFields(schema map[string]any) []string {
	rawRequired, ok := schema["required"]
	if !ok {
		return nil
	}

	switch required := rawRequired.(type) {
	case []string:
		return required
	case []any:
		result := make([]string, 0, len(required))
		for _, item := range required {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

func requiredFieldSet(schema map[string]any) map[string]bool {
	fields := requiredFields(schema)
	result := make(map[string]bool, len(fields))
	for _, field := range fields {
		result[field] = true
	}
	return result
}

func schemaMinLength(schema map[string]any) (int, bool) {
	raw, ok := schema["minLength"]
	if !ok {
		return 0, false
	}
	switch n := raw.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := strconv.Atoi(n.String())
		return i, err == nil
	default:
		return 0, false
	}
}

func validateEnum(schema map[string]any, value any, path string) []toolInputIssue {
	rawEnum, ok := schema["enum"]
	if !ok {
		return nil
	}

	var enum []any
	switch values := rawEnum.(type) {
	case []string:
		enum = make([]any, 0, len(values))
		for _, value := range values {
			enum = append(enum, value)
		}
	case []any:
		enum = values
	default:
		return nil
	}

	for _, allowed := range enum {
		if schemaValuesEqual(value, allowed) {
			return nil
		}
	}

	allowedValues := make([]string, 0, len(enum))
	for _, allowed := range enum {
		allowedValues = append(allowedValues, fmt.Sprintf("%v", allowed))
	}
	return []toolInputIssue{{Path: path, Message: fmt.Sprintf("must be one of: %s", strings.Join(allowedValues, ", "))}}
}

func schemaValuesEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		return av.String() == fmt.Sprintf("%v", b)
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

func isIntegerValue(value any) bool {
	switch v := value.(type) {
	case json.Number:
		if _, err := strconv.ParseInt(v.String(), 10, 64); err == nil {
			return true
		}
		f, err := strconv.ParseFloat(v.String(), 64)
		return err == nil && math.Trunc(f) == f
	case float64:
		return math.Trunc(v) == v
	default:
		return false
	}
}

func isNumberValue(value any) bool {
	switch v := value.(type) {
	case json.Number:
		_, err := strconv.ParseFloat(v.String(), 64)
		return err == nil
	case float64:
		return true
	default:
		return false
	}
}

func valueType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case bool:
		return "boolean"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func joinJSONPath(path, field string) string {
	if path == "" || path == "$" {
		return "$." + field
	}
	return path + "." + field
}

func isPathJSONPath(path string) bool {
	last := path
	if idx := strings.LastIndex(last, "."); idx >= 0 {
		last = last[idx+1:]
	}
	if idx := strings.Index(last, "["); idx >= 0 {
		last = last[:idx]
	}
	switch canonicalPropertyName(last) {
	case "path", "filepath", "absolutepath", "absolutefilepath":
		return true
	default:
		return false
	}
}

var markdownLinkPattern = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^)]+)\)`)

func hasDegenerateMarkdownLink(s string) bool {
	return unwrapDegenerateMarkdownLinks(s) != s
}

func unwrapDegenerateMarkdownLinks(s string) string {
	return markdownLinkPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := markdownLinkPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}

		text := strings.TrimSpace(parts[1])
		urlWithoutProtocol := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(parts[2]), "https://"), "http://")
		if normalizeLinkIdentity(text) != normalizeLinkIdentity(urlWithoutProtocol) {
			return match
		}
		return text
	})
}

func normalizeLinkIdentity(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "/")
	s = strings.ReplaceAll(s, " ", "")
	return strings.ToLower(s)
}
