package syntax

// The language table. Each entry is a comment style, a quoting style and two
// word lists — enough to drive six colours, which is what a terminal theme has.
// Anything not listed tokenizes as plain text rather than being guessed at.

func words(list ...string) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, w := range list {
		set[w] = true
	}
	return set
}

// cStyle is the shape shared by most curly-brace languages.
func cStyle(name string, keywords, types map[string]bool) Language {
	return Language{
		Name:        name,
		LineComment: []string{"//"},
		BlockOpen:   "/*",
		BlockClose:  "*/",
		Quotes:      "\"'",
		Keywords:    keywords,
		Types:       types,
	}
}

// hashStyle is the shape shared by the scripting languages.
func hashStyle(name string, keywords, types map[string]bool) Language {
	return Language{
		Name:        name,
		LineComment: []string{"#"},
		Quotes:      "\"'",
		Keywords:    keywords,
		Types:       types,
	}
}

var languages = map[string]Language{
	"go": withRaw('`', cStyle("go",
		words("break", "case", "chan", "const", "continue", "default", "defer",
			"else", "fallthrough", "for", "func", "go", "goto", "if", "import",
			"interface", "map", "package", "range", "return", "select", "struct",
			"switch", "type", "var", "nil", "true", "false", "iota"),
		words("string", "int", "int8", "int16", "int32", "int64", "uint",
			"uint8", "uint16", "uint32", "uint64", "float32", "float64", "bool",
			"byte", "rune", "error", "any", "complex64", "complex128", "uintptr"))),
	"js": withRaw('`', cStyle("javascript",
		words("async", "await", "break", "case", "catch", "class", "const",
			"continue", "debugger", "default", "delete", "do", "else", "export",
			"extends", "finally", "for", "from", "function", "if", "import",
			"in", "instanceof", "let", "new", "of", "return", "static", "super",
			"switch", "this", "throw", "try", "typeof", "var", "void", "while",
			"yield", "null", "undefined", "true", "false", "interface", "type",
			"enum", "implements", "readonly", "as", "declare", "namespace"),
		words("string", "number", "boolean", "object", "symbol", "bigint",
			"unknown", "never", "Array", "Promise", "Record", "Map", "Set"))),
	"rust": cStyle("rust",
		words("as", "async", "await", "break", "const", "continue", "crate",
			"dyn", "else", "enum", "extern", "fn", "for", "if", "impl", "in",
			"let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return",
			"self", "static", "struct", "super", "trait", "type", "unsafe",
			"use", "where", "while", "true", "false"),
		words("i8", "i16", "i32", "i64", "i128", "isize", "u8", "u16", "u32",
			"u64", "u128", "usize", "f32", "f64", "bool", "char", "str",
			"String", "Vec", "Option", "Result", "Box", "Some", "None", "Ok",
			"Err")),
	"java": cStyle("java",
		words("abstract", "assert", "break", "case", "catch", "class", "const",
			"continue", "default", "do", "else", "enum", "extends", "final",
			"finally", "for", "if", "implements", "import", "instanceof",
			"interface", "native", "new", "package", "private", "protected",
			"public", "return", "static", "super", "switch", "synchronized",
			"this", "throw", "throws", "transient", "try", "volatile", "while",
			"null", "true", "false", "val", "var", "fun", "object", "when"),
		words("boolean", "byte", "char", "double", "float", "int", "long",
			"short", "void", "String", "Object", "List", "Map", "Set")),
	"c": cStyle("c",
		words("auto", "break", "case", "const", "continue", "default", "do",
			"else", "enum", "extern", "for", "goto", "if", "inline", "register",
			"return", "sizeof", "static", "struct", "switch", "typedef", "union",
			"volatile", "while", "class", "namespace", "template", "public",
			"private", "protected", "virtual", "using", "nullptr", "true",
			"false", "new", "delete", "this", "operator"),
		words("char", "double", "float", "int", "long", "short", "signed",
			"unsigned", "void", "bool", "size_t", "uint8_t", "uint16_t",
			"uint32_t", "uint64_t", "int8_t", "int16_t", "int32_t", "int64_t")),
	"python": {
		Name:        "python",
		LineComment: []string{"#"},
		Quotes:      "\"'",
		Keywords: words("and", "as", "assert", "async", "await", "break",
			"class", "continue", "def", "del", "elif", "else", "except",
			"finally", "for", "from", "global", "if", "import", "in", "is",
			"lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try",
			"while", "with", "yield", "None", "True", "False", "self", "match",
			"case"),
		Types: words("int", "float", "str", "bool", "bytes", "list", "dict",
			"set", "tuple", "frozenset", "complex", "object", "type"),
	},
	"ruby": hashStyle("ruby",
		words("alias", "and", "begin", "break", "case", "class", "def",
			"defined?", "do", "else", "elsif", "end", "ensure", "false", "for",
			"if", "in", "module", "next", "nil", "not", "or", "redo", "rescue",
			"retry", "return", "self", "super", "then", "true", "unless",
			"until", "when", "while", "yield"),
		words("String", "Integer", "Float", "Array", "Hash", "Symbol", "Proc")),
	"shell": hashStyle("shell",
		words("if", "then", "else", "elif", "fi", "case", "esac", "for",
			"while", "until", "do", "done", "function", "return", "local",
			"export", "readonly", "declare", "source", "exit", "set", "unset",
			"shift", "trap", "eval", "exec"),
		nil),
	"yaml": hashStyle("yaml",
		words("true", "false", "null", "yes", "no", "on", "off"), nil),
	"sql": {
		Name:        "sql",
		LineComment: []string{"--"},
		BlockOpen:   "/*",
		BlockClose:  "*/",
		Quotes:      "'\"",
		Keywords: words("select", "from", "where", "insert", "into", "values",
			"update", "set", "delete", "join", "left", "right", "inner",
			"outer", "on", "group", "order", "by", "having", "limit", "offset",
			"create", "table", "index", "alter", "drop", "and", "or", "not",
			"null", "as", "distinct", "union", "with", "case", "when", "then",
			"else", "end", "primary", "key", "foreign", "references"),
		Types: words("int", "integer", "bigint", "smallint", "text", "varchar",
			"char", "boolean", "date", "timestamp", "numeric", "decimal",
			"json", "jsonb", "uuid"),
	},
	"json": {
		Name:     "json",
		Quotes:   "\"",
		Keywords: words("true", "false", "null"),
	},
	"css": {
		Name:       "css",
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quotes:     "\"'",
		Keywords: words("import", "media", "supports", "keyframes", "include",
			"mixin", "extend", "use", "if", "else", "each", "for", "function",
			"return"),
		Types: nil,
	},
}

// withRaw marks a language whose strings can span lines.
func withRaw(quote byte, lang Language) Language {
	lang.RawQuote = quote
	return lang
}

// byExtension maps a file extension to a language definition.
var byExtension = map[string]string{
	".go": "go",
	".js": "js", ".jsx": "js", ".ts": "js", ".tsx": "js", ".mjs": "js",
	".cjs": "js", ".vue": "js", ".svelte": "js",
	".rs":   "rust",
	".java": "java", ".kt": "java", ".kts": "java", ".scala": "java",
	".groovy": "java", ".swift": "java", ".cs": "java", ".dart": "java",
	".c": "c", ".h": "c", ".cc": "c", ".cpp": "c", ".hpp": "c", ".cxx": "c",
	".m": "c", ".mm": "c", ".hh": "c", ".proto": "c", ".php": "c",
	".py": "python", ".pyi": "python",
	".rb": "ruby", ".rake": "ruby", ".gemspec": "ruby",
	".sh": "shell", ".bash": "shell", ".zsh": "shell", ".fish": "shell",
	".yaml": "yaml", ".yml": "yaml", ".toml": "yaml", ".ini": "yaml",
	".cfg": "yaml", ".conf": "yaml", ".tf": "yaml", ".tfvars": "yaml",
	".sql":  "sql",
	".json": "json", ".jsonc": "json",
	".css": "css", ".scss": "css", ".sass": "css", ".less": "css",
}
