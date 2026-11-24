import { expect, test, type Page } from "@playwright/test";
import { loginAsAdmin } from "./support";

interface RoundMarkerSample {
  elapsedMs: number;
  x: number;
}

declare global {
  interface Window {
    roundMarkerError?: number;
    roundMarkerSamples?: RoundMarkerSample[];
  }
}

async function recordRoundMarker(page: Page) {
  await page.addInitScript(() => {
    let startedAt: number | null = null;
    let maxError = 0;
    const samples: RoundMarkerSample[] = [];
    const sample = (timestamp: number) => {
      const track = document.querySelector(".header-progress [role=progressbar]");
      const marker = track?.querySelector(".round-head")?.getBoundingClientRect();
      if (track && marker) {
        startedAt ??= timestamp;
        const elapsedMs = timestamp - startedAt;
        const progress = track.getAttribute("aria-valuenow");
        if (progress !== null && Number(progress) > 0) {
          const trackBox = track.getBoundingClientRect();
          const expected = trackBox.left + trackBox.width * Number(progress) / 100;
          maxError = Math.max(
            maxError,
            Math.abs(marker.left + marker.width / 2 - expected) / trackBox.width,
          );
        }
        samples.push({
          elapsedMs,
          x: marker.left + marker.width / 2,
        });
        if (elapsedMs >= 2100) {
          window.roundMarkerError = maxError;
          window.roundMarkerSamples = samples;
          return;
        }
      }
      requestAnimationFrame(sample);
    };
    requestAnimationFrame(sample);
  });
}

async function expectContinuousMotion(page: Page) {
  await expect.poll(async () => {
    return page.evaluate(() => {
      return window.roundMarkerSamples?.length ?? 0
    });
  }).toBeGreaterThan(0);

  const samples = await page.evaluate(() => {
    return window.roundMarkerSamples ?? []
  });
  const sampleAt = (elapsedMs: number) => {
    return samples.find((sample) => {
      return sample.elapsedMs >= elapsedMs
    });
  };
  for (const [startAt, middleAt, endAt] of [
    [100, 450, 800],
    [1100, 1450, 1800],
  ] as const) {
    const start = sampleAt(startAt);
    const middle = sampleAt(middleAt);
    const end = sampleAt(endAt);
    expect(start).toBeDefined();
    expect(middle).toBeDefined();
    expect(end).toBeDefined();
    expect(middle!.x).toBeGreaterThan(start!.x);
    expect(end!.x).toBeGreaterThan(middle!.x);
  }

  // ARIA uses whole-percent precision and can lag by one timer tick.
  await expect.poll(() => {
    return page.evaluate(() => {
      return window.roundMarkerError ?? Infinity
    });
  }).toBeLessThan(0.02);
}

async function markerX(page: Page) {
  const box = await page.locator(".header-progress .round-head").boundingBox();
  expect(box).not.toBeNull();
  return box!.x + box!.width / 2;
}

test("the round marker follows time from entry and reload", async ({ page }) => {
  await page.emulateMedia({
    reducedMotion: "no-preference",
  });
  await recordRoundMarker(page);
  await page.goto("/?demo=1");
  await expectContinuousMotion(page);

  await page.reload();
  await expectContinuousMotion(page);
  const time = page.locator(".header-progress .round-timer strong");
  await expect(time).not.toHaveText(await time.innerText());

  await page.locator(".demo-panel").getByRole("button", {
    name: "До старта",
    exact: true,
  }).click();
  await expect(page.locator(".header-progress .round-head")).toHaveCount(0);
});

test("the marker resets at a round boundary and pauses, resumes, and reduces motion", async ({ page }) => {
  await page.emulateMedia({
    reducedMotion: "no-preference",
  });
  await page.goto("/?demo=1");
  const marker = page.locator(".header-progress .round-head");
  await expect(marker).toBeVisible();

  const beforeRound = await markerX(page);
  await page.locator(".demo-panel").getByRole("button", {
    name: "Новое событие",
    exact: true,
  }).click();
  await expect.poll(() => {
    return markerX(page)
  }).toBeLessThan(beforeRound);
  const roundStart = await markerX(page);
  await page.waitForTimeout(250);
  expect(await markerX(page)).toBeGreaterThanOrEqual(roundStart);

  await loginAsAdmin(page, {
    username: "demo",
    password: "demo",
  });
  const pause = page.getByRole("button", {
    name: "Приостановить",
    exact: true,
  });
  await expect(pause).toBeVisible();
  await pause.click();
  await expect(page.getByRole("button", {
    name: "Продолжить",
    exact: true,
  })).toBeVisible();
  const pausedX = await markerX(page);
  await page.waitForTimeout(500);
  expect(await markerX(page)).toBe(pausedX);

  await page.getByRole("button", {
    name: "Продолжить",
    exact: true,
  }).click();
  const resumedX = await markerX(page);
  await page.waitForTimeout(500);
  expect(await markerX(page)).toBeGreaterThan(resumedX);

  const time = page.locator(".header-progress .round-timer strong");
  const beforeReducedMotion = await time.innerText();
  await page.emulateMedia({
    reducedMotion: "reduce",
  });
  await expect.poll(() => {
    return page.locator(".header-progress .round-head").evaluate((element) => {
      return element.getAnimations().map((animation) => {
        return animation.playState
      });
    });
  }).toEqual(["paused"]);
  await expect(time).not.toHaveText(beforeReducedMotion);
  const reducedMotionX = await markerX(page);
  await page.waitForTimeout(250);
  expect(await markerX(page)).toBe(reducedMotionX);

  await page.emulateMedia({
    reducedMotion: "no-preference",
  });
  await expect.poll(() => {
    return page.locator(".header-progress .round-head").evaluate((element) => {
      return element.getAnimations().map((animation) => {
        return animation.playState
      });
    });
  }).toEqual(["running"]);
  const motionRestoredX = await markerX(page);
  await page.waitForTimeout(500);
  expect(await markerX(page)).toBeGreaterThan(motionRestoredX);
});

test("a visibilitychange resynchronizes the marker after a wall-clock jump", async ({ page }) => {
  await page.goto("/?demo=1");
  await expect(page.locator(".header-progress .round-head")).toBeVisible();
  const before = await markerX(page);
  const track = await page.locator(".header-progress [role=progressbar]").boundingBox();
  expect(track).not.toBeNull();
  const now = await page.evaluate(() => {
    return Date.now()
  });
  await page.clock.setSystemTime(now + 10_000);
  await page.evaluate(() => {
    document.dispatchEvent(new Event("visibilitychange"));
  });
  // Ten seconds on the demo's 120-second round should advance about 1/12 of the track.
  await expect.poll(() => {
    return markerX(page)
  }).toBeGreaterThan(before + track!.width * 9 / 120);
  expect(await markerX(page)).toBeLessThan(before + track!.width * 12 / 120);
});
