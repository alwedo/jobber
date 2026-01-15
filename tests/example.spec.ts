import { test, expect } from "@playwright/test";

test("has title", async ({ page }) => {
  await page.goto("http://localhost:80");

  // Expect a title "to contain" a substring.
  await expect(page).toHaveTitle(/rssjobs/);
});

test("Create RSS feed", async ({ page }) => {
  await page.goto("http://localhost:80");

  await page.fill("input[name='keywords']", "barista");
  await page.fill("input[name='location']", "london");

  // Click the get started link.
  await page.getByRole("button", { name: "create RSS feed" }).click();

  // Expects page to have a heading with the name of Installation.
  await expect(
    // page.getByRole("paragraph", { name: "done! your RSS link is:" })
    page.getByText("done! your RSS link is:")
  ).toBeVisible();
});
