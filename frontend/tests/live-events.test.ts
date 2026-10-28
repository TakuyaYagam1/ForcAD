import assert from "node:assert/strict";
import test from "node:test";
import {
  mergeLiveEvents,
  notificationToLiveEvent,
  publicEventToLiveEvent,
} from "../src/entities/live-scoreboard/model/events.ts";

test("feed hydration replaces a matching live notification with its durable event", () => {
  const notification = notificationToLiveEvent(
    {
      event_id: "flag_stolen:flag:team",
      created_at: "2026-10-28T12:00:00Z",
      attacker_id: "attacker",
      victim_id: "victim",
      task_id: "task",
      attacker_delta: 12.5,
    },
    0,
  );
  const persisted = publicEventToLiveEvent({
    id: "flag_stolen:flag:team",
    kind: "flag_stolen",
    created_at: "2026-10-28T12:00:00Z",
    attacker_id: "attacker",
    attacker_name: "Red team",
    attacker_delta: 12.5,
    victim_id: "victim",
    victim_name: "Blue team",
    task_id: "task",
    task_name: "Web",
  });

  assert.ok(persisted);
  const events = mergeLiveEvents([notification], [persisted]);
  assert.equal(events.length, 1);
  assert.equal(events[0]?.attackerName, "Red team");
  assert.equal(events[0]?.id, notification.id);
});

test("old flag events without a stored delta keep the delta absent", () => {
  const event = publicEventToLiveEvent({
    id: "flag_stolen:old:team",
    kind: "flag_stolen",
    created_at: "2026-10-28T12:00:00Z",
    attacker_id: "attacker",
    victim_id: "victim",
    task_id: "task",
  });

  assert.ok(event);
  assert.equal("delta" in event ? event.delta : undefined, undefined);
});

test("hydrating an old row keeps the live delta already received for it", () => {
  const notification = notificationToLiveEvent(
    {
      event_id: "flag_stolen:old:team",
      created_at: "2026-10-28T12:00:00Z",
      attacker_id: "attacker",
      victim_id: "victim",
      task_id: "task",
      attacker_delta: 12.5,
    },
    0,
  );
  const persisted = publicEventToLiveEvent({
    id: notification.id,
    kind: "flag_stolen",
    created_at: "2026-10-28T12:00:00Z",
    attacker_id: "attacker",
    attacker_name: "Red team",
    victim_id: "victim",
    victim_name: "Blue team",
    task_id: "task",
    task_name: "Web",
  });

  assert.ok(persisted);
  const [event] = mergeLiveEvents([notification], [persisted]);
  assert.equal(event?.delta, 12.5);
  assert.equal(event?.attackerName, "Red team");
});

test("announcement content stays a text event and shares the bounded feed", () => {
  const announcement = publicEventToLiveEvent({
    id: "admin_message:one",
    kind: "admin_message",
    created_at: "2026-10-28T12:01:00Z",
    title: "Round update",
    markdown: "**Starts soon**",
    author: "admin",
  });

  assert.ok(announcement);
  assert.equal(announcement.kind, "admin_message");
  assert.equal(announcement.markdown, "**Starts soon**");
  const events = mergeLiveEvents([], [announcement]);
  assert.equal(events.length, 1);
  assert.equal(events[0]?.id, announcement.id);
});

test("feed merge sorts newest first and retains at most one hundred rows", () => {
  const current = Array.from({ length: 80 }, (_, i) => ({
    id: `old-${i}`,
    ts: i,
    attackerId: "attacker",
    victimId: "victim",
    taskId: "task",
    attackerName: "attacker",
    victimName: "victim",
    taskName: "task",
  }));
  const incoming = Array.from({ length: 30 }, (_, i) => ({
    id: `new-${i}`,
    ts: 100 + i,
    attackerId: "attacker",
    victimId: "victim",
    taskId: "task",
    attackerName: "attacker",
    victimName: "victim",
    taskName: "task",
  }));

  const events = mergeLiveEvents(current, incoming);
  assert.equal(events.length, 100);
  assert.equal(events[0]?.id, "new-29");
  assert.equal(events.at(-1)?.id, "old-10");
});
