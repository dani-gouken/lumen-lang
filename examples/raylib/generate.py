#!/usr/bin/env python3
"""Writes raylib.lum, the declarations of raylib for Lumen, from raylib.h.

    python3 examples/raylib/generate.py [path/to/raylib.h] > examples/raylib/raylib.lum

Every function of the header is declared with the C types it has, every
struct with its fields, every value of an enum as a constant, and the
colors as constants. What cannot be said in
Lumen is left out and listed at the end: functions that take a function,
and those that take any number of arguments.

With a C compiler at hand, the size the structs are given here is checked
against the one the compiler gives them.
"""

import os
import re
import subprocess
import sys
import tempfile

HEADERS = ["/opt/homebrew/include/raylib.h", "/usr/local/include/raylib.h", "/usr/include/raylib.h"]

PRIMITIVES = {
    "char": "Int8", "signed char": "Int8", "unsigned char": "UInt8",
    "short": "Int16", "unsigned short": "UInt16",
    "int": "Int32", "unsigned int": "UInt32", "unsigned": "UInt32",
    "long": "Int64", "unsigned long": "UInt64", "long long": "Int64", "unsigned long long": "UInt64",
    "float": "Float32", "double": "Float64", "bool": "Bool",
}
SIZES = {
    "Int8": (1, 1), "UInt8": (1, 1), "Bool": (1, 1), "Int16": (2, 2), "UInt16": (2, 2),
    "Int32": (4, 4), "UInt32": (4, 4), "Float32": (4, 4),
    "Int64": (8, 8), "UInt64": (8, 8), "Float64": (8, 8), "Pointer": (8, 8),
}
# Words a field or a parameter cannot be called in Lumen
RESERVED = set("""if else match case while for in break continue defer return fn let const struct enum
trait impl import as pub extern true false null is self default""".split())


def name_of(name):
    return name + "_" if name in RESERVED else name


class Header:
    def __init__(self, text):
        self.text = text
        self.structs = {}       # name -> [(field, type)], in the order of the header
        self.aliases = {}       # Texture2D -> Texture
        self.enums = []         # (name, [(constant, value, comment)])
        self.enum_names = set()
        self.callbacks = set()
        self.docs = {}          # struct -> comment
        self.read_types()

    def read_types(self):
        text = self.text
        for match in re.finditer(r"typedef\s+(\w+)\s+(\w+);", text):
            self.aliases[match.group(2)] = match.group(1)
        for match in re.finditer(r"typedef\s+[\w\s\*]+\(\*(\w+)\)\s*\(", text):
            self.callbacks.add(match.group(1))

        for match in re.finditer(r"(?://[ ]*([^\n]*)\n)?typedef struct (\w+) \{(.*?)\n\} (\w+);", text, re.S):
            comment, _, body, name = match.groups()
            fields = []
            for line in body.split("\n"):
                line = line.split("//")[0].strip().rstrip(";")
                if not line or line.startswith("#"):
                    continue
                # The type is everything up to the first name: "unsigned char *data", "float m0, m4"
                parts = line.rsplit(" ", 1) if "," not in line else None
                if parts is None:
                    first, rest = line.split(",", 1)
                    ctype, first_name = first.rsplit(" ", 1)
                    names = [first_name] + [n.strip() for n in rest.split(",")]
                else:
                    ctype, names = parts[0], [parts[1]]
                for declarator in names:
                    stars = declarator.count("*")
                    declarator = declarator.replace("*", "")
                    count = 1
                    array = re.match(r"(\w+)\[(\w+)\]$", declarator)
                    if array:
                        declarator, count = array.group(1), int(array.group(2))
                    field_type = ctype + " " + "*" * stars
                    if count == 1:
                        fields.append((declarator, field_type))
                    else:
                        # C has the values in a row: so does a field for each
                        fields.extend((declarator + str(i), field_type) for i in range(count))
            self.structs[name] = fields
            self.docs[name] = comment or ""

        for match in re.finditer(r"(?://[ ]*([^\n]*)\n)?typedef enum \{(.*?)\n\} (\w+);", text, re.S):
            comment, body, name = match.groups()
            values, next_value = [], 0
            for line in body.split("\n"):
                note = line.split("//", 1)[1].strip() if "//" in line else ""
                line = line.split("//")[0].strip().rstrip(",")
                if not line:
                    continue
                if "=" in line:
                    constant, value = [part.strip() for part in line.split("=")]
                    next_value = int(value, 0)
                else:
                    constant = line
                values.append((constant, next_value, note))
                next_value += 1
            self.enums.append((name, comment or "", values))
            self.enum_names.add(name)

    def resolve(self, name):
        while name in self.aliases and name not in self.structs:
            name = self.aliases[name]
        return name

    def lumen_type(self, ctype, is_result=False, in_struct=False):
        """The Lumen type of a C type, "" for void, or None if it has none."""
        const = re.search(r"\bconst\b", ctype) is not None
        stars = ctype.count("*")
        base = " ".join(re.sub(r"\bconst\b|\*", " ", ctype).split())
        if stars == 0:
            if base == "void":
                return "" if is_result else None
            if base in PRIMITIVES:
                return PRIMITIVES[base]
            if base in self.enum_names:
                return "Int32"
            if base in self.callbacks:
                return None
            base = self.resolve(base)
            if base in self.structs:
                return base
            return None
        if base in self.callbacks:
            return None
        # A text C only reads; any other address is for std.ffi
        if stars == 1 and base == "char" and const and not in_struct:
            return "CString"
        return "Pointer"

    def layout(self, name):
        """(size, alignment) of a struct as it is declared here."""
        size, align = 0, 1
        for _, ctype in self.structs[name]:
            lumen = self.lumen_type(ctype, in_struct=True)
            field_size, field_align = SIZES[lumen] if lumen in SIZES else self.layout(lumen)
            size = (size + field_align - 1) // field_align * field_align + field_size
            align = max(align, field_align)
        return (size + align - 1) // align * align, align


def generate(path):
    header = Header(open(path).read())
    version = re.search(r'#define RAYLIB_VERSION\s+"([^"]+)"', header.text)
    out = []
    emit = out.append

    emit("// raylib, for Lumen: every function, struct and constant of raylib.h.")
    emit("//")
    emit("// Written by generate.py from the header of raylib %s; change that, not this." % (version.group(1) if version else ""))
    emit("//")
    emit("//   import raylib;")
    emit("//")
    emit("//   raylib.InitWindow(800, 450, \"hello\");")
    emit("//   while !raylib.WindowShouldClose() {")
    emit("//       raylib.BeginDrawing();")
    emit("//       raylib.ClearBackground(raylib.RAYWHITE);")
    emit("//       raylib.DrawText(\"hello from Lumen\", 190, 200, 20, raylib.DARKGRAY);")
    emit("//       raylib.EndDrawing();")
    emit("//   }")
    emit("//   raylib.CloseWindow();")
    emit("//")
    emit("// The names are those of C, so what is written about raylib anywhere")
    emit("// applies. The types are those of C too: an Int32 is an Int to the")
    emit("// program, a Float32 a Float, a CString a String. A Pointer is an address,")
    emit("// an Int: see std.ffi for the functions that want one. A struct is passed")
    emit("// and returned by value, as raylib does.")
    emit("//")
    emit("// The library is found by its name: libraylib.dylib or libraylib.so, in")
    emit("// the usual places or in a folder listed in LUMEN_LIBRARY_PATH.")
    emit("")

    emit("// --- Structs ---")
    emit("")
    for name, fields in header.structs.items():
        if header.docs[name]:
            emit("// " + header.docs[name])
        emit("pub extern struct %s {" % name)
        for field, ctype in fields:
            emit("    %s: %s;" % (name_of(field), header.lumen_type(ctype, in_struct=True)))
        emit("}")
        emit("")
    for alias, target in header.aliases.items():
        if header.resolve(alias) in header.structs:
            emit("// %s is another name of %s in C: use %s." % (alias, header.resolve(alias), header.resolve(alias)))
    emit("")

    emit("// --- Colors ---")
    emit("")
    for match in re.finditer(r"#define (\w+)\s+CLITERAL\(Color\)\{\s*(\d+),\s*(\d+),\s*(\d+),\s*(\d+)\s*\}\s*(?://\s*(.*))?", header.text):
        constant, r, g, b, a, note = match.groups()
        emit("pub const %s = Color { r: %s, g: %s, b: %s, a: %s };%s" % (constant, r, g, b, a, "  // " + note if note else ""))
    emit("")

    emit("// --- Constants ---")
    for name, comment, values in header.enums:
        emit("")
        emit("// %s%s" % (name, ": " + comment if comment else ""))
        for constant, value, note in values:
            emit("pub const %s = %d;%s" % (constant, value, "  // " + note if note else ""))
    emit("")

    emit("// --- Functions ---")
    emit("")
    skipped = []
    for line in header.text.split("\n"):
        if not line.startswith("RLAPI"):
            if re.match(r"^// [A-Z][\w ,:/\-\(\)]*$", line) and out and not out[-1].startswith("// "):
                emit("")
                emit(line)
                emit("")
            continue
        declaration, _, note = line[len("RLAPI"):].partition("//")
        declaration = declaration.strip().rstrip(";")
        head, _, params = declaration.partition("(")
        params = params.rsplit(")", 1)[0].strip()
        function = re.search(r"(\w+)\s*$", head).group(1)
        result = header.lumen_type(head[:head.rindex(function)], is_result=True)

        declared, why = [], None
        if result is None:
            why = "its result"
        for i, param in enumerate([] if params in ("void", "") else params.split(",")):
            param = param.strip()
            if param == "...":
                why = "takes any number of arguments"
                break
            named = re.match(r"(.*?)(\w+)$", param)
            lumen = header.lumen_type(named.group(1))
            if lumen is None:
                why = "takes a function" if named.group(1).strip() in header.callbacks else "the type of '%s'" % named.group(2)
                break
            declared.append("%s: %s" % (name_of(named.group(2)), lumen))
        if why:
            skipped.append((function, why))
            continue
        if note.strip():
            emit("// " + note.strip())
        emit('pub extern "raylib" fn %s(%s)%s;' % (function, ", ".join(declared), " -> " + result if result else ""))

    emit("")
    emit("// --- Left out ---")
    emit("//")
    for function, why in skipped:
        emit("//   %s: %s" % (function, why))

    check_sizes(header, path)
    # One empty line at most between two things
    tidy = [line for i, line in enumerate(out) if line or (i > 0 and out[i - 1])]
    return "\n".join(tidy) + "\n"


def check_sizes(header, path):
    """Compares the size each struct has here with the one C gives it."""
    source = '#include <stdio.h>\n#include "%s"\nint main(void) {\n' % path
    for name in header.structs:
        source += '    printf("%s %%zu\\n", sizeof(%s));\n' % (name, name)
    source += "    return 0;\n}\n"
    with tempfile.TemporaryDirectory() as folder:
        program = os.path.join(folder, "sizes")
        with open(program + ".c", "w") as file:
            file.write(source)
        try:
            subprocess.run(["cc", "-o", program, program + ".c"], check=True, capture_output=True)
            sizes = subprocess.run([program], check=True, capture_output=True, text=True).stdout
        except (OSError, subprocess.CalledProcessError) as problem:
            print("the sizes of the structs were not checked: %s" % problem, file=sys.stderr)
            return
    wrong = 0
    for line in sizes.split("\n"):
        if line:
            name, size = line.split()
            if header.layout(name)[0] != int(size):
                print("%s is %s bytes in C, %d here" % (name, size, header.layout(name)[0]), file=sys.stderr)
                wrong += 1
    print("%d structs, %d of a size C does not agree with" % (len(header.structs), wrong), file=sys.stderr)
    if wrong:
        sys.exit(1)


if __name__ == "__main__":
    path = sys.argv[1] if len(sys.argv) > 1 else next((h for h in HEADERS if os.path.exists(h)), None)
    if path is None:
        sys.exit("raylib.h was not found: give its path")
    sys.stdout.write(generate(path))
