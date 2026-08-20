package kittygraphics

import (
	"fmt"
)

// ParseControls parses the comma-separated control header of an APC.
func ParseControls(header []byte) (Controls, error) { return parseControls(header) }

// ParseHeader is an alias for ParseControls.
func ParseHeader(header []byte) (Controls, error) { return ParseControls(header) }

func parseControls(header []byte) (Controls, error) {
	var c Controls
	if len(header) == 0 {
		return c, nil
	}
	seen := make(map[byte]struct{}, 8)
	for _, field := range splitComma(header) {
		if len(field) < 3 || field[1] != '=' {
			return Controls{}, fmt.Errorf("%w: control %q", ErrInvalidCommand, field)
		}
		key := field[0]
		if _, ok := seen[key]; ok {
			return Controls{}, fmt.Errorf("%w: %c", ErrDuplicateControl, key)
		}
		seen[key] = struct{}{}
		value := field[2:]
		if err := parseControl(&c, key, value); err != nil {
			return Controls{}, err
		}
	}
	if c.HasAction && !c.Action.Valid() {
		return Controls{}, fmt.Errorf("%w: %q", ErrUnknownAction, c.Action)
	}
	if c.HasImageID && c.HasImageNumber {
		return Controls{}, fmt.Errorf("%w: image id and image number are mutually exclusive", ErrInvalidCommand)
	}
	if c.HasQuiet && c.Quiet > QuietAll {
		return Controls{}, fmt.Errorf("%w: quiet=%d", ErrInvalidCommand, c.Quiet)
	}
	if c.HasMore && c.More > 1 {
		return Controls{}, fmt.Errorf("%w: m=%d", ErrInvalidCommand, c.More)
	}
	if c.HasCursor && c.Cursor > 1 {
		return Controls{}, fmt.Errorf("%w: C=%d", ErrInvalidCommand, c.Cursor)
	}
	return c, nil
}

func splitComma(data []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, b := range data {
		if b == ',' {
			out = append(out, data[start:i])
			start = i + 1
		}
	}
	return append(out, data[start:])
}

func parseControl(c *Controls, key byte, value []byte) error {
	if len(value) == 0 {
		return fmt.Errorf("%w: empty %c value", ErrInvalidInteger, key)
	}
	switch ControlKey(key) {
	case ControlAction:
		if len(value) != 1 {
			return fmt.Errorf("%w: action %q", ErrUnknownAction, value)
		}
		c.Action, c.HasAction = Action(value[0]), true
	case ControlCompression:
		n, err := parseUint(value)
		if err != nil {
			return fieldError(key, err)
		}
		c.Compression, c.HasCompression = n, true
	case ControlFormat:
		n, err := parseUint(value)
		if err != nil {
			return fieldError(key, err)
		}
		f := Format(n)
		if !f.Valid() {
			return fmt.Errorf("%w: f=%d", ErrInvalidCommand, n)
		}
		c.Format, c.HasFormat = f, true
	case ControlImageID:
		return setID(&c.ImageID, &c.HasImageID, key, value)
	case ControlImageNumber:
		return setID(&c.ImageNumber, &c.HasImageNumber, key, value)
	case ControlMore:
		return setUint(&c.More, &c.HasMore, key, value)
	case ControlQuiet:
		var n uint64
		if err := setUint(&n, nil, key, value); err != nil {
			return err
		}
		c.Quiet, c.HasQuiet = Quiet(n), true
	case ControlWidth:
		return setUint(&c.Width, &c.HasWidth, key, value)
	case ControlHeight:
		return setUint(&c.Height, &c.HasHeight, key, value)
	case ControlColumns:
		return setUint(&c.Columns, &c.HasColumns, key, value)
	case ControlRows:
		return setUint(&c.Rows, &c.HasRows, key, value)
	case ControlX:
		return setUint(&c.X, &c.HasX, key, value)
	case ControlY:
		return setUint(&c.Y, &c.HasY, key, value)
	case ControlSourceWidth:
		return setUint(&c.SourceWidth, &c.HasSourceWidth, key, value)
	case ControlSourceHeight:
		return setUint(&c.SourceHeight, &c.HasSourceHeight, key, value)
	case ControlLayer:
		return setInt(&c.Layer, &c.HasLayer, key, value)
	case ControlPlacementID:
		return setID(&c.PlacementID, &c.HasPlacementID, key, value)
	case ControlDelete:
		if len(value) != 1 {
			return fmt.Errorf("%w: d=%q", ErrInvalidCommand, value)
		}
		d := DeleteTarget(value[0])
		switch d {
		case DeleteImage, DeleteImageNumber, DeletePlacement, DeleteAll, DeleteAllImages, DeleteAllPlacements:
			c.Delete, c.HasDelete = d, true
		default:
			return fmt.Errorf("%w: d=%q", ErrInvalidCommand, value)
		}
	case ControlTransmission:
		if len(value) != 1 || Transmission(value[0]) != TransmissionDirect {
			return fmt.Errorf("%w: t=%q", ErrUnsupported, value)
		}
		c.Transmission, c.HasTransmission = TransmissionDirect, true
	case ControlCursor:
		return setUint(&c.Cursor, &c.HasCursor, key, value)
	case ControlSourceX:
		return setUint(&c.SourceX, &c.HasSourceX, key, value)
	case ControlSourceY:
		return setUint(&c.SourceY, &c.HasSourceY, key, value)
	case ControlCellOffsetX:
		return setInt(&c.CellOffsetX, &c.HasCellOffsetX, key, value)
	case ControlCellOffsetY:
		return setInt(&c.CellOffsetY, &c.HasCellOffsetY, key, value)
	case ControlParent:
		return setUint(&c.Parent, &c.HasParent, key, value)
	default:
		return fmt.Errorf("%w: %c", ErrUnknownControl, key)
	}
	return nil
}

func fieldError(key byte, err error) error {
	return fmt.Errorf("%w: %c: %w", ErrInvalidCommand, key, err)
}

func setID(dst *uint64, present *bool, key byte, value []byte) error {
	if err := setUint(dst, present, key, value); err != nil {
		return err
	}
	if *dst > uint64(^uint32(0)) {
		return fieldError(key, ErrIntegerOverflow)
	}
	return nil
}

func setUint(dst *uint64, present *bool, key byte, value []byte) error {
	n, err := parseUint(value)
	if err != nil {
		return fieldError(key, err)
	}
	*dst = n
	if present != nil {
		*present = true
	}
	return nil
}

func setInt(dst *int64, present *bool, key byte, value []byte) error {
	n, err := parseInt(value)
	if err != nil {
		return fieldError(key, err)
	}
	*dst = n
	if present != nil {
		*present = true
	}
	return nil
}

func parseUint(value []byte) (uint64, error) {
	if len(value) == 0 {
		return 0, ErrInvalidInteger
	}
	var n uint64
	for _, b := range value {
		if b < '0' || b > '9' {
			return 0, ErrInvalidInteger
		}
		digit := uint64(b - '0')
		if n > (^uint64(0)-digit)/10 {
			return 0, ErrIntegerOverflow
		}
		n = n*10 + digit
	}
	return n, nil
}

func parseInt(value []byte) (int64, error) {
	if len(value) == 0 {
		return 0, ErrInvalidInteger
	}
	negative := value[0] == '-'
	if negative {
		value = value[1:]
		if len(value) == 0 {
			return 0, ErrInvalidInteger
		}
	}
	n, err := parseUint(value)
	if err != nil {
		return 0, err
	}
	if negative {
		if n > uint64(1)<<63 {
			return 0, ErrIntegerOverflow
		}
		if n == uint64(1)<<63 {
			return -1 << 63, nil
		}
		return -int64(n), nil
	}
	if n > uint64(^uint64(0)>>1) {
		return 0, ErrIntegerOverflow
	}
	return int64(n), nil
}
