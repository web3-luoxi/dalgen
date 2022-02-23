package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"text/template"

	"github.com/xwb1989/sqlparser"
)

var (
	databaseName string
	outputDir    string
)

const tableTemplate = `
package {{.Package}}

{{.Imports}}

type {{.TableName}} struct {
{{.Columns}}
}

func ({{.TableName}}) TableName() string {
	return "{{.TableNameStr}}"
}
`

func init() {
	flag.StringVar(&databaseName, "database", "model", "database's name")
	flag.StringVar(&outputDir, "output", "", "output directory")
}

func ParseSQLs(content string) ([]*sqlparser.DDL, error) {
	pieces, err := sqlparser.SplitStatementToPieces(content)
	if err != nil {
		return nil, err
	}
	ddls := make([]*sqlparser.DDL, 0, len(pieces))
	for _, piece := range pieces {
		stmt, err := sqlparser.Parse(piece)
		if err != nil {
			continue
		}
		switch stmt.(type) {
		case *sqlparser.DDL:
			ddl := stmt.(*sqlparser.DDL)
			if ddl.Action != "create" {
				continue
			}
			if ddl.TableSpec == nil {
				continue
			}
			ddls = append(ddls, ddl)
		}
	}
	return ddls, nil
}

func ToCamelFirstUpper(str string) string {
	pieces := strings.Split(str, "_")
	newPieces := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		newPieces = append(newPieces, strings.Title(piece))

	}
	return strings.Join(newPieces, "")
}

type Column struct {
	Name    string
	Type    string
	Comment string
}

func (c Column) String() string {
	s := fmt.Sprintf("%s %s `gorm:\"Column:%s\" json:\"%s\"`",
		ToCamelFirstUpper(c.Name), c.Type, c.Name, c.Name)
	if c.Comment == "" {
		return s
	} else {
		return s + "// " + c.Comment
	}
}

//GenColumn
func GenColumn(c *sqlparser.ColumnDefinition) string {
	switch c.Type.Type {
	case "bigint":
		return Column{c.Name.String(), "int64", getComment(c)}.String()
	case "int", "smallint", "tinyint":
		return Column{c.Name.String(), "int", getComment(c)}.String()
	case "char", "varchar", "text", "mediumtext", "longtext":
		return Column{c.Name.String(), "string", getComment(c)}.String()
	case "blob":
		return Column{c.Name.String(), "[]byte", getComment(c)}.String()
	case "float", "double", "decimal":
		return Column{c.Name.String(), "float64", getComment(c)}.String()
	case "bit":
		return Column{c.Name.String(), "uint64", getComment(c)}.String()
	case "date", "datetime", "timestamp":
		return Column{c.Name.String(), "time.Time", getComment(c)}.String()
	default:
		panic(fmt.Sprintf("bad Column: %+v", c))
	}
}

func getComment(c *sqlparser.ColumnDefinition) string {
	if c == nil {
		return ""
	}
	if c.Type.Comment == nil {
		return ""
	} else {
		return string(c.Type.Comment.Val)
	}
}

func getFilePath(tableName string) string {
	pwd, _ := os.Getwd()

	p := pwd
	if outputDir != "" {
		p = path.Join(p, outputDir)
	}
	if databaseName != "" {
		p = path.Join(p, databaseName)
	}
	p = path.Join(p, fmt.Sprintf("%+v.go", tableName))
	fmt.Println(p)
	return p
}

func genTable(pkg string, ddl *sqlparser.DDL) string {
	var imports string
	if needTimeImport(ddl) {
		imports = `import "time"` + "\n"
	}

	tableNameStr := ddl.NewName.Name.String()
	tableName := ToCamelFirstUpper(tableNameStr)

	var columns strings.Builder
	for i, c := range genColumns(ddl) {
		if i != 0 {
			columns.WriteString("\n")
		}
		columns.WriteString("\t")
		columns.WriteString(c)
	}

	params := struct {
		Package      string
		Imports      string
		TableName    string
		TableNameStr string
		Columns      string
	}{
		Package:      pkg,
		Imports:      imports,
		TableName:    tableName,
		TableNameStr: tableNameStr,
		Columns:      columns.String(),
	}

	var buf bytes.Buffer
	_ = template.Must(template.New("header").Parse(tableTemplate)).Execute(&buf, params)

	return buf.String()
}

func needTimeImport(ddl *sqlparser.DDL) bool {
	for _, c := range ddl.TableSpec.Columns {
		switch c.Type.Type {
		case "date", "datetime", "timestamp":
			return true
		}
	}
	return false
}

func genColumns(ddl *sqlparser.DDL) []string {
	columns := make([]string, 0, len(ddl.TableSpec.Columns))
	for _, c := range ddl.TableSpec.Columns {
		columns = append(columns, GenColumn(c))
	}
	return columns
}

func gen(file string, pkgName string) error {
	content, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	ddls, err := ParseSQLs(string(content))
	if err != nil {
		return err
	}
	pkg := "model"
	if pkgName != "" {
		pkg = pkgName
	}
	for _, ddl := range ddls {
		fp := getFilePath(ddl.NewName.Name.String())
		dir, _ := path.Split(fp)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			os.MkdirAll(dir, os.ModePerm)
		}
		if err := ioutil.WriteFile(fp, []byte(genTable(pkg, ddl)), 0755); err != nil {
			return err
		}
		cmd := exec.Command("go", "fmt", fp)
		cmd.Env = os.Environ()
		if err := cmd.Run(); err != nil {
			fmt.Printf("go fmt failed: %v\n", err)
		}
	}
	return nil
}

func main() {
	flag.Parse()
	sqlFileName := flag.Arg(0)
	if err := gen(sqlFileName, databaseName); err != nil {
		fmt.Println(err)
	}
}
