// #4191: a save in the rule editor deleted the actions that the rule switches off.
const shown = { field: "NAME", operator: "CONTAINS", value: "shown" }
const other = { field: "RATIO", operator: "GREATER_THAN", value: "3" }

export default async function (t) {
  const rules = `/api/instances/${t.instanceId}/automations`
  const created = await t.api("POST", rules, {
    name: "Made-up rule", trackerPattern: "*", enabled: false,
    conditions: {
      schemaVersion: "1",
      tags: [
        { enabled: true, tags: ["running"], mode: "full", condition: shown },
        { enabled: false, tags: ["later"], mode: "add", condition: other },
      ],
      speedLimits: { enabled: false, uploadKiB: 512, condition: other },
    },
  })
  const switchedOff = c => ({ tags: c.tags?.filter(a => !a.enabled), speedLimits: c.speedLimits })

  await t.goto("/automations")
  await t.page.getByText("0/1 active").click()
  await t.page.locator("button:has(svg.lucide-pencil)").click()
  await t.page.getByRole("dialog").getByLabel("Name", { exact: true }).fill("Renamed rule")
  const saved = t.page.waitForResponse(r => r.request().method() === "PUT" && r.url().includes(`/automations/${created.id}`))
  await t.page.getByRole("dialog").getByRole("button", { name: "Save", exact: true }).click()
  if (!(await saved).ok()) throw new Error(`the save answered ${(await saved).status()}`)

  const after = (await t.api("GET", rules)).find(r => r.id === created.id)
  if (after.name !== "Renamed rule") throw new Error("the rename did not reach the rule")
  await t.page.getByRole("dialog").waitFor({ state: "detached" })
  await t.page.locator("button:has(svg.lucide-pencil) + button").click()
  await t.page.getByRole("menuitem", { name: "Edit as JSON" }).click()
  await t.page.getByRole("dialog").waitFor()
  await t.shot("rule JSON after save")
  t.assert.deepEqual(switchedOff(after.conditions), switchedOff(created.conditions))
}
