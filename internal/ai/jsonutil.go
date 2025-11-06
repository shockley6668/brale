package ai

import "strings"

// ExtractJSONArrayCompat 提取首个 JSON 数组，返回数组文本与是否成功
func ExtractJSONArrayCompat(s string) (string, bool) {
    start := strings.Index(s, "[")
    if start == -1 {
        return "", false
    }
    depth := 0
    for i := start; i < len(s); i++ {
        switch s[i] {
        case '[':
            depth++
        case ']':
            depth--
            if depth == 0 {
                return strings.TrimSpace(s[start : i+1]), true
            }
        }
    }
    return "", false
}

// ExtractJSONArrayWithIndex 提取首个 JSON 数组，返回数组文本、起始下标与是否成功
func ExtractJSONArrayWithIndex(s string) (string, int, bool) {
    start := strings.Index(s, "[")
    if start == -1 {
        return "", -1, false
    }
    depth := 0
    for i := start; i < len(s); i++ {
        switch s[i] {
        case '[':
            depth++
        case ']':
            depth--
            if depth == 0 {
                return strings.TrimSpace(s[start : i+1]), start, true
            }
        }
    }
    return "", -1, false
}

