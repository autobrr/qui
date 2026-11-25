import assert from "node:assert/strict"
import { test } from "node:test"
import { twMerge } from "tailwind-merge"

import { compare } from "./class-lists.mjs"

const plain = { Label: { options: {}, classes: ({ className }) => twMerge("flex gap-2 text-sm leading-none font-medium", className) } }
const sized = {
  Label: {
    options: { size: ["default", "sm"] },
    classes: ({ size = "default", className }) => twMerge("flex gap-2 font-medium", size === "sm" ? "text-xs" : "text-sm leading-none", className),
  },
}

const header = "import { Label } from \"@/components/ui/label\"\n"
const side = (primitives, body) => ({ source: `${header}${body}`, primitives, cn: twMerge })
const run = (before, after) => compare(side(plain, before), side(sized, after))
const verdicts = (before, after) => run(before, after).map(({ changed, same }) => ({ changed, same }))

test("a deleted repeat of a default keeps the merged class list", () => {
  assert.deepEqual(verdicts("const a = <Label className=\"font-medium mb-1\" />", "const a = <Label className=\"mb-1\" />"), [{ changed: true, same: true }])
})

test("a deleted text-sm changes the merged class list, because it removed the tight line height", () => {
  const [result] = run("const a = <Label className=\"text-sm\" />", "const a = <Label />")
  assert.equal(result.same, false)
  assert.deepEqual(result.difference, { lost: [], gained: ["leading-none"] })
})

test("classes that moved into an option keep the merged class list", () => {
  assert.deepEqual(verdicts("const a = <Label className=\"text-xs text-muted-foreground\" />", "const a = <Label size=\"sm\" className=\"text-muted-foreground\" />"), [{ changed: true, same: true }])
})

test("an element that the change does not touch is compared too, because its primitive can change", () => {
  const thicker = { Label: { options: {}, classes: ({ className }) => twMerge("flex gap-2 text-sm leading-none font-bold", className) } }
  const body = "const a = <Label className=\"mb-1\" />"
  const [result] = compare(side(plain, body), side(thicker, body))
  assert.deepEqual({ changed: result.changed, same: result.same }, { changed: false, same: false })
})

test("each branch of a condition is compared, also when the condition moves to another prop", () => {
  const before = "const look = small ? \"text-xs\" : warn ? \"text-red-500\" : \"\"\nconst a = <Label className={look} />"
  assert.deepEqual(verdicts(before, "const look = !small && warn ? \"text-red-500\" : undefined\nconst a = <Label size={small ? \"sm\" : \"default\"} className={look} />"), [{ changed: true, same: true }])
  assert.deepEqual(verdicts(before, "const a = <Label size={small ? \"sm\" : \"default\"} className={warn ? \"text-red-500\" : undefined} />"), [{ changed: true, same: false }])
})

test("a class value that the script cannot read keeps its place in the list", () => {
  assert.deepEqual(verdicts("const a = <Label className={cn(\"text-xs\", extra(row))} />", "const a = <Label size=\"sm\" className={extra(row)} />"), [{ changed: true, same: true }])
  assert.deepEqual(verdicts("const a = <Label className={`text-xs ${extra(row)} mb-1`} />", "const a = <Label size=\"sm\" className={`${extra(row)} mb-1`} />"), [{ changed: true, same: true }])
  assert.deepEqual(verdicts("const a = <Label className={cn(extra(row), \"text-xs\")} />", "const a = <Label size=\"sm\" className={extra(row)} />"), [{ changed: true, same: false }])
  assert.deepEqual(verdicts("const a = <Label className={extra(row)} />", "const a = <Label className={extra(other)} />"), [{ changed: true, same: false }])
})

test("a name that a parameter shadows is not read from the outer scope", () => {
  const before = "const look = \"text-xs\"\nconst a = (look) => <Label className={look} />"
  assert.deepEqual(verdicts(before, "const a = (look) => <Label size=\"sm\" />"), [{ changed: true, same: false }])
})

test("a loop variable that shadows a constant is not read from the outer scope", () => {
  const before = "const look = \"text-xs\"\nfor (const look of looks) list.push(<Label className={look} />)"
  assert.deepEqual(verdicts(before, "for (const look of looks) list.push(<Label size=\"sm\" />)"), [{ changed: true, same: false }])
})

test("a class value that the script cannot read differs when its definition differs", () => {
  const before = "const looks = { a: \"text-xs\" }\nconst a = <Label className={looks[key]} />"
  assert.deepEqual(verdicts(before, before), [{ changed: false, same: true }])
  assert.deepEqual(verdicts(before, "const looks = { a: \"\" }\nconst a = <Label className={looks[key]} />"), [{ changed: false, same: false }])
  const call = "function look(row) { return row ? \"text-xs\" : \"\" }\nconst a = <Label className={look(row)} />"
  assert.deepEqual(verdicts(call, call.replace("text-xs", "")), [{ changed: false, same: false }])
})

test("a condition differs when its definition differs", () => {
  const before = "const small = count > 1\nconst a = <Label className={small ? \"text-xs\" : \"\"} />"
  assert.deepEqual(verdicts(before, "const small = count > 1\nconst a = <Label size={small ? \"sm\" : \"default\"} />"), [{ changed: true, same: true }])
  assert.deepEqual(verdicts(before, "const small = count > 2\nconst a = <Label size={small ? \"sm\" : \"default\"} />"), [{ changed: true, same: false }])
})

test("an option that the script cannot read is compared with each value that it can have", () => {
  const body = "const a = <Label size={pick(row)} className=\"text-xs\" />"
  const shrink = { Label: { ...sized.Label, classes: ({ size = "default", className }) => twMerge(size === "sm" ? "p-1" : "p-2", className) } }
  assert.deepEqual(compare(side(sized, body), side(sized, body)).map(({ same }) => same), [true])
  assert.deepEqual(compare(side(sized, body), side(shrink, body)).map(({ same }) => same), [false])
})

test("a null option turns the option off, so it differs from an omitted one", () => {
  const nullable = { Label: { ...sized.Label, classes: ({ size = "default", className }) => twMerge("flex", size && (size === "sm" ? "text-xs" : "text-sm"), className) } }
  const same = (before, after) => compare(side(nullable, before), side(nullable, after)).map((result) => result.same)
  assert.deepEqual(same("const a = <Label size={null} />", "const a = <Label />"), [false])
  assert.deepEqual(same("const a = <Label size={small ? null : \"sm\"} />", "const a = <Label size={small ? undefined : \"sm\"} />"), [false])
  assert.deepEqual(same("const a = <Label size={null} className=\"text-sm\" />", "const a = <Label className=\"text-sm\" />"), [true])
  assert.deepEqual(same("const a = <Label className={cn(null, \"mb-1\")} />", "const a = <Label className=\"mb-1\" />"), [true])
})

test("a spread differs when its definition differs", () => {
  const look = "const look = { className: \"text-xs\" }\n"
  const element = "const a = <Label {...look} />"
  assert.deepEqual(verdicts(look + element, look + element), [{ changed: false, same: true }])
  assert.deepEqual(verdicts(look + element, look.replace("xs", "lg") + element), [{ changed: false, same: false }])
  assert.deepEqual(verdicts(look + element, `${look}const a = <Label {...look} className={look.className} />`), [{ changed: false, same: true }])
  const late = "const a = <Label className={base} {...look} />"
  assert.deepEqual(verdicts(look + late, look.replace("xs", "lg") + late), [{ changed: false, same: false }])
  assert.deepEqual(verdicts(`const base = "mb-1"\n${look}${late}`, `const base = "mb-2"\n${look}${late}`), [{ changed: false, same: false }])
})

test("a spread gives its class to an element that names none", () => {
  assert.deepEqual(verdicts("const a = <Label {...look} className={cn(\"font-medium\", look.className)} />", "const a = <Label {...look} className={look.className} />"), [{ changed: true, same: true }])
  assert.deepEqual(verdicts("const a = <Label {...look} className={cn(\"font-medium\", look.className)} />", "const a = <Label {...look} />"), [{ changed: true, same: true }])
})

test("a file with another number of elements cannot be compared", () => {
  assert.throws(() => run("const a = <Label />", "const a = [<Label />, <Label />]"), /Label: 1 before, 2 after/)
})

test("an element of the same name from another module is left out", () => {
  const body = "const a = <Label className=\"text-xs\" />"
  const other = { source: `import { Label } from "recharts"\n${body}`, primitives: plain, cn: twMerge }
  assert.deepEqual(compare(other, other), [])
})
