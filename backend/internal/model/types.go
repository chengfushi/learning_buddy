// package model —— 自定义类型，处理 PostgreSQL 数组映射。
package model

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// StringArray 映射 PostgreSQL TEXT[]（如 tags、weak_points）。
// 以 {a,b,c} 文本格式读写，避免引入额外依赖。
type StringArray []string

// GormDataType 告知 GORM 该自定义类型的列类型，使其走 Valuer/Scanner 而非按切片处理。
func (StringArray) GormDataType() string { return "text[]" }

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return nil, nil
	}
	parts := make([]string, len(a))
	for i, s := range a {
		parts[i] = `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func (a *StringArray) Scan(src interface{}) error {
	if src == nil {
		*a = nil
		return nil
	}
	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("StringArray: unsupported scan type %T", src)
	}
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		*a = StringArray{}
		return nil
	}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	// 简化解析：按逗号切分，去掉引号（生产可换成健壮的数组解析）。
	if s == "" {
		*a = StringArray{}
		return nil
	}
	fields := strings.Split(s, ",")
	out := make(StringArray, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		f = strings.Trim(f, `"`)
		out = append(out, f)
	}
	*a = out
	return nil
}
