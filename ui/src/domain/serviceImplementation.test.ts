import { describe, expect, it } from 'bun:test';

import { serviceImplementation, storedWebAddress, storedWorkerTopic, webAddress, workerTopic } from './serviceImplementation';

// The same cases as tests/bpmn/service_task_implementation_test.go: what the
// panel shows is what the engine runs.
describe('serviceImplementation', () => {
  it('lets the recorded choice outvote a topic typed before it', () => {
    const data = { implementation: 'push', externalTopic: 'carrier-worker', httpUrl: 'https://carrier.example/notify' };
    expect(serviceImplementation(data)).toBe('push');
    expect(workerTopic(data)).toBe('');
    expect(webAddress(data)).toBe('https://carrier.example/notify');
  });

  it('reads a web address saved under its older name', () => {
    expect(webAddress({ implementation: 'push', url: 'https://carrier.example/notify' })).toBe('https://carrier.example/notify');
  });

  it('reads a topic saved under its older name', () => {
    expect(workerTopic({ implementation: 'external', topic: 'ship-parcel' })).toBe('ship-parcel');
  });

  it('reads past a field the modeller cleared', () => {
    expect(storedWorkerTopic({ externalTopic: '', topic: 'ship-parcel' })).toBe('ship-parcel');
    expect(storedWebAddress({ httpUrl: '', url: 'https://carrier.example/notify' })).toBe('https://carrier.example/notify');
  });

  it('infers the choice for a step imported with none recorded', () => {
    expect(serviceImplementation({ external_topic: 'ship-parcel' })).toBe('external');
    expect(serviceImplementation({ connector_id: 'slack' })).toBe('connector');
    expect(serviceImplementation({})).toBe('push');
  });

  it('gives a worker step no address and a web step no topic', () => {
    const worker = { implementation: 'external', externalTopic: 'ship-parcel', http_url: 'https://stale.example' };
    expect(webAddress(worker)).toBe('');
    expect(workerTopic(worker)).toBe('ship-parcel');
  });
});
