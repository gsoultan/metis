/**
 * The printed report, rendered as markup.
 */
import { describe, expect, it } from 'bun:test';
import { renderToStaticMarkup } from 'react-dom/server';

import { heatFromWaiting } from '../../domain/processHeatmap';
import { slaReport } from '../../domain/slaReport';
import { ProcessReport, type ProcessReportFigures } from './ProcessReport';

const now = new Date('2026-09-26T09:00:00Z');

/** Twelve late tasks across three people: the dashboard lists the top five groups, not the tasks. */
const lateTasks = Array.from({ length: 12 }, (_, i) => ({
  id: `task-${i}`,
  name: `Approve invoice ${i}`,
  assignee: ['ana', 'budi', 'citra'][i % 3],
  processName: 'Invoice approval',
  dueDate: new Date(now.getTime() - (i + 1) * 3_600_000).toISOString(),
  status: 'OPEN',
}));

/** Eight busy steps: the dashboard draws the first six. */
const waiting = [
  {
    key: 'invoice',
    name: 'Invoice approval',
    instances: 40,
    steps: Array.from({ length: 8 }, (_, i) => ({ node_id: `Task_Step${i}`, waiting: 8 - i })),
  },
];

function figures(overrides: Partial<ProcessReportFigures> = {}): ProcessReportFigures {
  return {
    projectName: 'Finance',
    generatedAt: now,
    activeInstances: 40,
    processModels: 3,
    completion: { done: 30, total: 40, rate: 75 },
    needsAttention: 2,
    report: slaReport(lateTasks, now),
    heat: heatFromWaiting(waiting),
    ...overrides,
  };
}

const rendered = (f: ProcessReportFigures) => renderToStaticMarkup(<ProcessReport figures={f} />);

describe('ProcessReport', () => {
  it('states the headline figures', () => {
    const html = rendered(figures());
    expect(html).toContain('<h1>Finance</h1>');
    expect(html).toContain('Active instances</th><td>40</td>');
    expect(html).toContain('Process models</th><td>3</td>');
    expect(html).toContain('75% (30 of 40)');
    expect(html).toContain('Needs attention</th><td>2</td>');
  });

  // The dashboard is for noticing; the report is what somebody acts on, so it
  // leaves nothing out.
  it('lists every late task and every busy step, not the top few', () => {
    const html = rendered(figures());
    for (const task of lateTasks) {
      expect(html).toContain(`<td>${task.name}</td>`);
    }
    for (let i = 0; i < 8; i += 1) {
      expect(html).toContain(`<td>Step${i}</td>`);
    }
    expect(html).toContain('12 tasks are past their deadline');
  });

  // Names are typed by people, and the report is rendered into a frame of the
  // app's own origin: one must print as what it says, never run.
  it('prints a hostile name as text', () => {
    const hostile = '<img src=x onerror=alert(1)>';
    const html = rendered(
      figures({
        projectName: hostile,
        report: slaReport([{ ...lateTasks[0], name: hostile, assignee: hostile, processName: hostile }], now),
        heat: heatFromWaiting([{ key: 'k', name: hostile, instances: 1, steps: [{ node_id: 'Task_A', waiting: 1 }] }]),
      }),
    );
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
  });

  it('says there is nothing late or waiting rather than printing empty tables', () => {
    const html = rendered(figures({ report: slaReport([], now), heat: [] }));
    expect(html).toContain('No open work with a deadline.');
    expect(html).not.toContain('Every late task');
    expect(html).not.toContain('<caption>Invoice approval');
  });
});
