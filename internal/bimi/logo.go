package bimi

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Checking that a logo is the restricted kind of SVG a mark has to be.
//
// BIMI does not accept any SVG. It accepts SVG Tiny Portable/Secure: a profile
// with no script, no event handlers, no animation, no external references, no
// embedded photographs and no hyperlinks. A file that breaks the profile is
// refused by the receiver, which says nothing to anybody — the mark simply
// never appears, and the sender has no way to learn why.
//
// So this refuses it here, naming what is wrong, which is the whole value of
// checking at all.
//
// The check is also what makes it safe to serve the file. An SVG is a document
// rather than a picture: served from this server's own name, one carrying a
// script would run with this server's origin. The media store elsewhere in
// this program refuses SVG outright for that reason and says so. A file that
// satisfies the profile has nothing in it that runs, so the check the
// specification demands is the same check that makes hosting it safe, and
// there is no second sanitiser to keep in step with this one.

const (
	// MaximumLogoSize is what will be stored and served. The specification
	// asks for a mark under 32KB, and a mark is a few paths.
	MaximumLogoSize = 32 << 10
)

// Logo is what a valid file turned out to say about itself.
type Logo struct {
	// Title is the <title> the profile requires, which is what a reader with
	// a screen reader hears in place of the mark.
	Title string
}

// elementsForbidden are the ones that make a document do something rather than
// show something.
var elementsForbidden = map[string]string{
	"script":           "a script",
	"foreignobject":    "foreign content",
	"image":            "an embedded image",
	"animate":          "animation",
	"animatetransform": "animation",
	"animatemotion":    "animation",
	"animatecolor":     "animation",
	"set":              "animation",
	"a":                "a link",
	"use":              "a reference to another element",
	"iframe":           "a frame",
	"style":            "a style element",
	"filter":           "a filter",
}

// ValidateLogo reports why a file cannot be published as a mark, or nil when
// it can.
//
// The order is the order somebody would want to hear it: whether it is an SVG
// at all, then whether it declares the profile, then what is in it. The first
// failure is the one returned, because a file with six problems is a file to
// go back to the designer about rather than a list to work through.
func ValidateLogo(content []byte) (*Logo, error) {
	if len(content) == 0 {
		return nil, errors.New("the file is empty")
	}
	if len(content) > MaximumLogoSize {
		return nil, fmt.Errorf("the file is %d bytes; a mark has to be under %d", len(content), MaximumLogoSize)
	}

	decoder := xml.NewDecoder(strings.NewReader(string(content)))
	// A document that pulls in an external entity is a document that fetches
	// something when it is parsed, which is the oldest trick there is.
	decoder.Entity = map[string]string{}
	decoder.Strict = true

	logo := &Logo{}
	root := false
	depth := 0
	inTitle := false
	var title strings.Builder

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the file is not valid XML: %w", err)
		}

		switch element := token.(type) {
		case xml.ProcInst:
			// The XML declaration is ordinary; anything else is a program.
			if !strings.EqualFold(element.Target, "xml") {
				return nil, fmt.Errorf("the file carries a %q instruction", element.Target)
			}
		case xml.Directive:
			// <!DOCTYPE ...> can declare entities, and this parser has been
			// told to resolve none — but a document that needs one is not a
			// mark.
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(string(element))), "DOCTYPE") {
				return nil, errors.New("the file has a DOCTYPE, which a mark does not use")
			}
		case xml.StartElement:
			depth++
			name := strings.ToLower(element.Name.Local)
			if depth == 1 {
				if name != "svg" {
					return nil, fmt.Errorf("the file begins with <%s> rather than <svg>", name)
				}
				root = true
				if err := checkRoot(element); err != nil {
					return nil, err
				}
				continue
			}
			if what, forbidden := elementsForbidden[name]; forbidden {
				return nil, fmt.Errorf("the file contains %s, which a mark may not have (<%s>)", what, name)
			}
			if name == "title" && depth == 2 {
				inTitle = true
			}
			if err := checkAttributes(element); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if strings.EqualFold(element.Name.Local, "title") {
				inTitle = false
			}
			depth--
		case xml.CharData:
			if inTitle {
				title.Write(element)
			}
		}
	}

	if !root {
		return nil, errors.New("the file has no <svg> element")
	}
	logo.Title = strings.TrimSpace(title.String())
	if logo.Title == "" {
		// Required by the profile, and the thing somebody using a screen
		// reader hears where everybody else sees the mark.
		return nil, errors.New("the file has no <title>, which the profile requires and a screen reader reads")
	}
	return logo, nil
}

// checkRoot reads what the <svg> element itself has to say: the profile it
// claims, and the square it draws in.
func checkRoot(element xml.StartElement) error {
	profile, version, viewBox := "", "", ""
	for _, attribute := range element.Attr {
		switch strings.ToLower(attribute.Name.Local) {
		case "baseprofile":
			profile = strings.ToLower(strings.TrimSpace(attribute.Value))
		case "version":
			version = strings.TrimSpace(attribute.Value)
		case "viewbox":
			viewBox = strings.TrimSpace(attribute.Value)
		}
	}
	if profile != "tiny-ps" {
		return errors.New(`the <svg> element does not say baseProfile="tiny-ps", which is what marks a file as this profile`)
	}
	if version != "" && version != "1.2" {
		return fmt.Errorf("the <svg> element says version %q; the profile is version 1.2", version)
	}
	if viewBox == "" {
		return errors.New("the <svg> element has no viewBox, so a receiver cannot scale the mark")
	}
	return checkSquare(viewBox)
}

// checkSquare refuses a mark that is not square. Receivers draw one in a
// square, and a wide logo comes out either squashed or cropped.
func checkSquare(viewBox string) error {
	parts := strings.FieldsFunc(viewBox, func(letter rune) bool { return letter == ' ' || letter == ',' })
	if len(parts) != 4 {
		return fmt.Errorf("the viewBox %q is not four numbers", viewBox)
	}
	width, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return fmt.Errorf("the viewBox width %q is not a number", parts[2])
	}
	height, err := strconv.ParseFloat(parts[3], 64)
	if err != nil {
		return fmt.Errorf("the viewBox height %q is not a number", parts[3])
	}
	if width <= 0 || height <= 0 {
		return fmt.Errorf("the viewBox %q has no area", viewBox)
	}
	if width != height {
		return fmt.Errorf("the mark is %gx%g; it has to be square, or a receiver will crop it", width, height)
	}
	return nil
}

// checkAttributes refuses the two things an element can carry that make it do
// something: a handler, and an address somewhere else.
func checkAttributes(element xml.StartElement) error {
	for _, attribute := range element.Attr {
		name := strings.ToLower(attribute.Name.Local)
		if strings.HasPrefix(name, "on") {
			return fmt.Errorf("the file has a %q handler on <%s>, which a mark may not have",
				attribute.Name.Local, element.Name.Local)
		}
		if name == "href" || name == "xlink:href" {
			return fmt.Errorf("the file refers to %q; a mark may not point anywhere outside itself", attribute.Value)
		}
		value := strings.ToLower(attribute.Value)
		if strings.Contains(value, "url(http") || strings.Contains(value, "url(//") ||
			strings.Contains(value, "javascript:") || strings.Contains(value, "data:") {
			return fmt.Errorf("the file refers to something outside itself in %q", attribute.Name.Local)
		}
	}
	return nil
}
