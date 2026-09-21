package sqliteexporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

func groupSpansAsOTLPResourceSpans(spans []json.RawMessage) []interface{} {
	// Group by the complete resource/scope identity, including schema URLs.
	type scopeKey struct {
		service string
		scope   string
	}
	resources := make(map[string]map[string][]map[string]interface{})
	resourceAttrs := make(map[string][]map[string]interface{})
	scopeAttrs := make(map[scopeKey]map[string]interface{})
	resourceMetadata := make(map[string]map[string]interface{})
	scopeMetadata := make(map[scopeKey]map[string]interface{})

	for _, raw := range spans {
		var m map[string]interface{}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&m); err != nil {
			continue
		}

		service := ""
		if res, ok := m["resource"].(map[string]interface{}); ok {
			if v, ok := res["service.name"].(string); ok {
				service = v
			}
		}
		if service == "" {
			if v, ok := m["service_name"].(string); ok {
				service = v
			}
		}
		if service == "" {
			service = "unknown"
		}

		resource, _ := m["resource"].(map[string]interface{})
		if resource == nil {
			resource = map[string]interface{}{}
		}
		if _, ok := resource["service.name"]; !ok {
			resource["service.name"] = service
		}
		resourceKey, _ := json.Marshal([]interface{}{resource, m["resource_schema_url"], m["resource_dropped_attributes_count"]})
		service = string(resourceKey)
		resourceAttrs[service] = mapToOTLPAttributes(resource)
		resourceMetadata[service] = m

		scopeName := ""
		if scope, ok := m["scope"].(map[string]interface{}); ok {
			key, _ := json.Marshal([]interface{}{scope, m["scope_schema_url"]})
			scopeName = string(key)
			converted := map[string]interface{}{}
			copyField(converted, "name", scope, "name")
			copyField(converted, "version", scope, "version")
			copyField(converted, "droppedAttributesCount", scope, "dropped_attributes_count")
			if a, ok := scope["attributes"].(map[string]interface{}); ok {
				converted["attributes"] = mapToOTLPAttributes(a)
			}
			scopeAttrs[scopeKey{service: service, scope: scopeName}] = converted
		}

		if _, ok := resources[service]; !ok {
			resources[service] = make(map[string][]map[string]interface{})
		}

		scopeMetadata[scopeKey{service: service, scope: scopeName}] = m
		otlpSpan := toOTLPSpan(m)
		resources[service][scopeName] = append(resources[service][scopeName], otlpSpan)
	}

	out := make([]interface{}, 0, len(resources))
	resourceKeys := make([]string, 0, len(resources))
	for key := range resources {
		resourceKeys = append(resourceKeys, key)
	}
	sort.Strings(resourceKeys)
	for _, service := range resourceKeys {
		scopes := resources[service]
		scopeKeys := make([]string, 0, len(scopes))
		for key := range scopes {
			scopeKeys = append(scopeKeys, key)
		}
		sort.Strings(scopeKeys)
		scopeSpans := make([]interface{}, 0, len(scopes))
		for _, scopeName := range scopeKeys {
			key := scopeKey{service: service, scope: scopeName}
			scope := scopeAttrs[key]
			if scope == nil {
				scope = map[string]interface{}{}
			}
			ss := map[string]interface{}{"scope": scope, "spans": scopes[scopeName]}
			copyField(ss, "schemaUrl", scopeMetadata[key], "scope_schema_url")
			scopeSpans = append(scopeSpans, ss)
		}
		res := map[string]interface{}{"attributes": resourceAttrs[service]}
		copyField(res, "droppedAttributesCount", resourceMetadata[service], "resource_dropped_attributes_count")
		rs := map[string]interface{}{"resource": res, "scopeSpans": scopeSpans}
		copyField(rs, "schemaUrl", resourceMetadata[service], "resource_schema_url")
		out = append(out, rs)
	}

	return out
}

func toOTLPSpan(m map[string]interface{}) map[string]interface{} {
	traceID, _ := m["trace_id"].(string)
	spanID, _ := m["span_id"].(string)
	parentSpanID, _ := m["parent_span_id"].(string)
	name, _ := m["span_name"].(string)
	kind, _ := m["kind"].(string)

	start := decimalInteger(m["start_time_unix_nano"])
	end := decimalInteger(m["end_time_unix_nano"])

	attrs := []map[string]interface{}{}
	if a, ok := m["attributes"].(map[string]interface{}); ok {
		attrs = mapToOTLPAttributes(a)
	}

	status := map[string]interface{}{}
	if st, ok := m["status"].(map[string]interface{}); ok {
		code := "STATUS_CODE_UNSET"
		{
			switch decimalInteger(st["code"]) {
			case "1":
				code = "STATUS_CODE_OK"
			case "2":
				code = "STATUS_CODE_ERROR"
			}
		}
		status["code"] = code
		if msg, ok := st["message"].(string); ok && msg != "" {
			status["message"] = msg
		}
	}

	otlpKind := "SPAN_KIND_UNSPECIFIED"
	switch strings.ToLower(kind) {
	case "internal":
		otlpKind = "SPAN_KIND_INTERNAL"
	case "server":
		otlpKind = "SPAN_KIND_SERVER"
	case "client":
		otlpKind = "SPAN_KIND_CLIENT"
	case "producer":
		otlpKind = "SPAN_KIND_PRODUCER"
	case "consumer":
		otlpKind = "SPAN_KIND_CONSUMER"
	}

	out := map[string]interface{}{
		"traceId":           traceID,
		"spanId":            spanID,
		"name":              name,
		"kind":              otlpKind,
		"startTimeUnixNano": start,
		"endTimeUnixNano":   end,
		"attributes":        attrs,
		"status":            status,
	}
	if parentSpanID != "" && parentSpanID != "0000000000000000" {
		out["parentSpanId"] = parentSpanID
	}

	if evs, ok := m["events"].([]interface{}); ok {
		converted := make([]map[string]interface{}, 0, len(evs))
		for _, ev := range evs {
			em, ok := ev.(map[string]interface{})
			if !ok {
				continue
			}
			ce := map[string]interface{}{}
			if n, ok := em["name"].(string); ok {
				ce["name"] = n
			}
			ce["timeUnixNano"] = decimalInteger(em["timestamp"])
			copyField(ce, "droppedAttributesCount", em, "dropped_attributes_count")
			if at, ok := em["attributes"].(map[string]interface{}); ok {
				ce["attributes"] = mapToOTLPAttributes(at)
			}
			converted = append(converted, ce)
		}
		if len(converted) > 0 {
			out["events"] = converted
		}
	}

	copyField(out, "traceState", m, "trace_state")
	copyField(out, "flags", m, "flags")
	copyField(out, "droppedAttributesCount", m, "dropped_attributes_count")
	copyField(out, "droppedEventsCount", m, "dropped_events_count")
	copyField(out, "droppedLinksCount", m, "dropped_links_count")
	if links, ok := m["links"].([]interface{}); ok {
		converted := make([]map[string]interface{}, 0, len(links))
		for _, link := range links {
			if lm, ok := link.(map[string]interface{}); ok {
				l := map[string]interface{}{"traceId": lm["trace_id"], "spanId": lm["span_id"]}
				copyField(l, "traceState", lm, "trace_state")
				copyField(l, "flags", lm, "flags")
				copyField(l, "droppedAttributesCount", lm, "dropped_attributes_count")
				if a, ok := lm["attributes"].(map[string]interface{}); ok {
					l["attributes"] = mapToOTLPAttributes(a)
				}
				converted = append(converted, l)
			}
		}
		out["links"] = converted
	}
	return out
}

func copyField(dst map[string]interface{}, key string, src map[string]interface{}, from string) {
	if value, ok := src[from]; ok {
		dst[key] = value
	}
}

// Stored JSON is decoded with UseNumber. float64 support is for legacy callers;
// precision already lost before conversion cannot be recovered.
func decimalInteger(v interface{}) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case float64:
		return strconv.FormatFloat(n, 'f', 0, 64)
	case nil:
		return "0"
	default:
		return fmt.Sprint(n)
	}
}

func mapToOTLPAttributes(m map[string]interface{}) []map[string]interface{} {
	attrs := make([]map[string]interface{}, 0, len(m))
	for k, v := range m {
		attrs = append(attrs, map[string]interface{}{
			"key":   k,
			"value": toOTLPAnyValue(v),
		})
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i]["key"].(string) < attrs[j]["key"].(string) })
	return attrs
}

func toOTLPAnyValue(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case string:
		return map[string]interface{}{"stringValue": t}
	case bool:
		return map[string]interface{}{"boolValue": t}
	case float64:
		// JSON numbers decode as float64.
		if math.Mod(t, 1) == 0 {
			return map[string]interface{}{"intValue": fmt.Sprintf("%d", int64(t))}
		}
		return map[string]interface{}{"doubleValue": t}
	case float32:
		return map[string]interface{}{"doubleValue": float64(t)}
	case int:
		return map[string]interface{}{"intValue": fmt.Sprintf("%d", t)}
	case int64:
		return map[string]interface{}{"intValue": fmt.Sprintf("%d", t)}
	case []interface{}:
		values := make([]map[string]interface{}, 0, len(t))
		for _, item := range t {
			values = append(values, toOTLPAnyValue(item))
		}
		return map[string]interface{}{"arrayValue": map[string]interface{}{"values": values}}
	case map[string]interface{}:
		return map[string]interface{}{"kvlistValue": map[string]interface{}{"values": mapToOTLPAttributes(t)}}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return map[string]interface{}{"intValue": fmt.Sprintf("%d", i)}
		}
		if f, err := t.Float64(); err == nil {
			return map[string]interface{}{"doubleValue": f}
		}
		return map[string]interface{}{"stringValue": t.String()}
	default:
		return map[string]interface{}{"stringValue": fmt.Sprintf("%v", v)}
	}
}
