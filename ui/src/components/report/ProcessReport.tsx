import type { TaskCompletion } from '../../domain/dashboardFigures';
import { heatSummary, type ProcessHeat } from '../../domain/processHeatmap';
import { slaSummary, type SlaReport } from '../../domain/slaReport';

/**
 * What the report says: the dashboard's figures, as the dashboard read them.
 *
 * Passed in rather than fetched again, so the printed page and the screen it
 * was printed from cannot disagree, and neither can the CSV exports beside them.
 */
export interface ProcessReportFigures {
  projectName: string;
  generatedAt: Date;
  activeInstances: number;
  processModels: number;
  completion: TaskCompletion;
  needsAttention: number;
  report: SlaReport;
  heat: ProcessHeat[];
}

/**
 * The dashboard as a document: something to attach to an email or file with
 * the minutes, which a screen of cards is not.
 *
 * Plain elements rather than Mantine's: it is printed from a frame of its own,
 * where Mantine's styles are not, and a report that depends on a stylesheet it
 * does not carry prints as a jumble. Everything is listed, where the dashboard
 * shows the top few: the dashboard is for noticing, the report for acting on.
 */
export function ProcessReport({ figures }: { figures: ProcessReportFigures }) {
  const { report, heat, completion } = figures;
  return (
    <article className="report">
      <header>
        <h1>{figures.projectName}</h1>
        <p className="meta">Process report, {formatGenerated(figures.generatedAt)}</p>
      </header>

      <section>
        <h2>At a glance</h2>
        <table className="figures">
          <tbody>
            <tr><th scope="row">Active instances</th><td>{figures.activeInstances}</td></tr>
            <tr><th scope="row">Process models</th><td>{figures.processModels}</td></tr>
            <tr>
              <th scope="row">Tasks completed</th>
              <td>{completion.rate}% ({completion.done} of {completion.total})</td>
            </tr>
            <tr><th scope="row">Needs attention</th><td>{figures.needsAttention}</td></tr>
          </tbody>
        </table>
      </section>

      <section>
        <h2>Deadlines</h2>
        <p>{slaSummary(report)}</p>
        {report.breached.length > 0 && (
          <>
            <BreachTable caption="Who is carrying it" heading="Person" groups={report.byAssignee} />
            <BreachTable caption="Which process" heading="Process" groups={report.byProcess} />
            <table>
              <caption>Every late task</caption>
              <thead>
                <tr><th>Task</th><th>Process</th><th>Assignee</th><th>Due</th><th>Late by</th></tr>
              </thead>
              <tbody>
                {report.breached.map((task) => (
                  <tr key={task.id}>
                    <td>{task.name}</td>
                    <td>{task.processName}</td>
                    <td>{task.assignee}</td>
                    <td>{task.dueDate}</td>
                    <td>{task.lateBy}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </section>

      <section>
        <h2>Where work is waiting</h2>
        <p>{heatSummary(heat)}</p>
        {heat.map((process) => (
          <table key={process.processName}>
            <caption>
              {process.processName}: {process.instances} running {process.instances === 1 ? 'instance' : 'instances'}
            </caption>
            <thead>
              <tr><th>Step</th><th className="count">Waiting</th><th className="bar-cell" aria-hidden="true" /></tr>
            </thead>
            <tbody>
              {process.nodes.map((node) => (
                <tr key={node.nodeId}>
                  <td>{node.label}</td>
                  <td className="count">{node.waiting}</td>
                  <td className="bar-cell" aria-hidden="true">
                    <span className="bar" style={{ width: `${Math.round(node.intensity * 100)}%` }} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ))}
      </section>

      <footer>
        <p>
          Counted by the server when this report was made: the same figures as the dashboard and its CSV exports at
          that moment.
        </p>
      </footer>
    </article>
  );
}

function BreachTable({ caption, heading, groups }: { caption: string; heading: string; groups: SlaReport['byAssignee'] }) {
  return (
    <table>
      <caption>{caption}</caption>
      <thead>
        <tr><th>{heading}</th><th className="count">Late</th><th>Worst</th></tr>
      </thead>
      <tbody>
        {groups.map((group) => (
          <tr key={group.name}>
            <td>{group.name}</td>
            <td className="count">{group.breached}</td>
            <td>{group.worstLateBy}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** When the report was made, to the minute, in the reader's own locale. */
function formatGenerated(at: Date): string {
  return at.toLocaleString(undefined, { dateStyle: 'long', timeStyle: 'short' });
}
