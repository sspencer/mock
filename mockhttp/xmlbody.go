package mockhttp

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"github.com/sspencer/mock/restclient"
)

var errBodyNotXML = errors.New("request body is not valid XML")

// xmlNode is one element of a decoded XML request body. content holds its
// character data (string) and child elements (*xmlNode) in document order.
type xmlNode struct {
	name    string
	attrs   map[string]string
	content []any
}

// text is the element's character data and that of its descendants, with
// surrounding whitespace trimmed.
func (n *xmlNode) text() string {
	var b strings.Builder
	var walk func(*xmlNode)
	walk = func(node *xmlNode) {
		for _, item := range node.content {
			switch item := item.(type) {
			case string:
				b.WriteString(item)
			case *xmlNode:
				walk(item)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// decodeXML parses an XML document into a tree. Element and attribute names
// are local names; namespace prefixes are dropped.
func decodeXML(data []byte) (*xmlNode, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var root *xmlNode
	var stack []*xmlNode
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errBodyNotXML
		}
		switch token := token.(type) {
		case xml.StartElement:
			node := &xmlNode{name: token.Name.Local, attrs: make(map[string]string, len(token.Attr))}
			for _, attr := range token.Attr {
				if attr.Name.Space == "xmlns" || attr.Name.Local == "xmlns" && attr.Name.Space == "" {
					continue
				}
				node.attrs[attr.Name.Local] = attr.Value
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, errBodyNotXML
				}
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.content = append(parent.content, node)
			}
			stack = append(stack, node)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.content = append(parent.content, string(token))
			} else if len(bytes.TrimSpace(token)) > 0 {
				return nil, errBodyNotXML
			}
		}
	}
	if root == nil {
		return nil, errBodyNotXML
	}
	return root, nil
}

// xmlValues looks up an element path in the XML body. The first segment names
// the root element and each later one a child element by local name; "*"
// selects every child element, and repeated elements are all selected. A final
// ["@name"] segment selects an attribute. Elements yield their trimmed text.
// A blank body has no fields; any other body that is not XML is an error, and
// then no @xml matcher matches.
func (f *requestFacts) xmlValues(path string) ([]string, error) {
	if f.body.unavailable {
		return nil, errBodyUnavailable
	}
	if !f.xmlDone {
		f.xmlDone = true
		if len(bytes.TrimSpace(f.body.data)) > 0 {
			f.xml, f.xmlErr = decodeXML(f.body.data)
		}
	}
	if f.xmlErr != nil {
		return nil, f.xmlErr
	}
	segments, err := restclient.ParseJSONPath(path)
	if err != nil || f.xml == nil {
		return nil, nil
	}
	nodes := []*xmlNode{{content: []any{f.xml}}} // a virtual document node
	for i, segment := range segments {
		if attr, ok := strings.CutPrefix(segment.Name, "@"); ok && segment.Quoted {
			if i != len(segments)-1 {
				return nil, nil
			}
			var values []string
			for _, node := range nodes {
				if value, ok := node.attrs[attr]; ok {
					values = append(values, value)
				}
			}
			return values, nil
		}
		var next []*xmlNode
		for _, node := range nodes {
			for _, item := range node.content {
				if child, ok := item.(*xmlNode); ok && (segment.Any && !segment.Quoted || child.name == segment.Name) {
					next = append(next, child)
				}
			}
		}
		nodes = next
	}
	values := make([]string, 0, len(nodes))
	for _, node := range nodes {
		values = append(values, node.text())
	}
	return values, nil
}

func (f *requestFacts) xmlMatches(path, expected string) bool {
	values, err := f.xmlValues(path)
	return err == nil && restclient.MustMatcher(expected).MatchAny(values)
}
