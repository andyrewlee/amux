package daytona

import "strings"

// Image represents a simple build context for snapshots.
type Image struct {
	dockerfile strings.Builder
}

// Dockerfile returns the generated Dockerfile.
func (i *Image) Dockerfile() string {
	return i.dockerfile.String()
}

// ImageBase creates an Image from an existing base image.
func ImageBase(image string) *Image {
	img := &Image{}
	img.dockerfile.WriteString("FROM ")
	img.dockerfile.WriteString(image)
	img.dockerfile.WriteString("\n")
	return img
}

// RunCommands appends RUN instructions to the Dockerfile.
func (i *Image) RunCommands(commands ...any) *Image {
	for _, command := range commands {
		switch v := command.(type) {
		case string:
			i.dockerfile.WriteString("RUN ")
			i.dockerfile.WriteString(v)
			i.dockerfile.WriteString("\n")
		case []string:
			quoted := make([]string, 0, len(v))
			for _, part := range v {
				part = strings.ReplaceAll(part, "\\", "\\\\")
				part = strings.ReplaceAll(part, "\"", "\\\"")
				part = strings.ReplaceAll(part, "'", "\\'")
				quoted = append(quoted, "\""+part+"\"")
			}
			i.dockerfile.WriteString("RUN ")
			i.dockerfile.WriteString(strings.Join(quoted, " "))
			i.dockerfile.WriteString("\n")
		}
	}
	return i
}
