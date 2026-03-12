import http from 'k6/http';
import { check, sleep, group } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || 'test-api-key';

// Custom metrics
const workflowCreateRate = new Rate('workflow_create_success');
const workflowTriggerRate = new Rate('workflow_trigger_success');
const workflowListDuration = new Trend('workflow_list_duration');
const runStatusDuration = new Trend('run_status_duration');

export const options = {
  stages: [
    { duration: '30s', target: 10 },   // Ramp up to 10 users
    { duration: '1m', target: 50 },    // Ramp up to 50 users
    { duration: '2m', target: 50 },    // Stay at 50 users
    { duration: '1m', target: 100 },   // Ramp up to 100 users
    { duration: '2m', target: 100 },   // Stay at 100 users
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
    http_req_failed: ['rate<0.05'],
    workflow_create_success: ['rate>0.95'],
    workflow_trigger_success: ['rate>0.95'],
  },
};

const headers = {
  'Content-Type': 'application/json',
  'Authorization': `Bearer ${API_KEY}`,
};

const workflowTemplate = {
  name: 'Load Test Workflow',
  description: 'Workflow created during load testing',
  tasks: [
    {
      id: 'start',
      name: 'Start Task',
      type: 'delay',
      config: { duration: '1s' },
    },
    {
      id: 'process',
      name: 'Process Task',
      type: 'http',
      depends_on: ['start'],
      config: {
        url: 'https://httpbin.org/post',
        method: 'POST',
        body: '{"test": true}',
      },
    },
    {
      id: 'complete',
      name: 'Complete Task',
      type: 'delay',
      depends_on: ['process'],
      config: { duration: '1s' },
    },
  ],
};

export default function () {
  group('Workflow CRUD', () => {
    // Create workflow
    const createPayload = JSON.stringify({
      ...workflowTemplate,
      name: `Load Test ${Date.now()}-${__VU}`,
    });

    const createRes = http.post(`${BASE_URL}/api/v1/workflows`, createPayload, { headers });
    workflowCreateRate.add(createRes.status === 201);
    check(createRes, {
      'workflow created': (r) => r.status === 201,
      'has workflow id': (r) => JSON.parse(r.body).id !== undefined,
    });

    if (createRes.status !== 201) {
      return;
    }

    const workflow = JSON.parse(createRes.body);
    const workflowId = workflow.id;

    // Get workflow
    const getRes = http.get(`${BASE_URL}/api/v1/workflows/${workflowId}`, { headers });
    check(getRes, {
      'workflow retrieved': (r) => r.status === 200,
      'correct workflow': (r) => JSON.parse(r.body).id === workflowId,
    });

    // List workflows
    const listStart = Date.now();
    const listRes = http.get(`${BASE_URL}/api/v1/workflows?limit=20&offset=0`, { headers });
    workflowListDuration.add(Date.now() - listStart);
    check(listRes, {
      'workflows listed': (r) => r.status === 200,
    });

    // Trigger workflow
    const triggerRes = http.post(`${BASE_URL}/api/v1/workflows/${workflowId}/trigger`, '{}', { headers });
    workflowTriggerRate.add(triggerRes.status === 200 || triggerRes.status === 201);
    check(triggerRes, {
      'workflow triggered': (r) => r.status === 200 || r.status === 201,
    });

    if (triggerRes.status === 200 || triggerRes.status === 201) {
      const run = JSON.parse(triggerRes.body);
      const runId = run.id;

      // Poll run status
      const statusStart = Date.now();
      const statusRes = http.get(`${BASE_URL}/api/v1/runs/${runId}`, { headers });
      runStatusDuration.add(Date.now() - statusStart);
      check(statusRes, {
        'run status retrieved': (r) => r.status === 200,
      });
    }

    // Delete workflow
    const deleteRes = http.del(`${BASE_URL}/api/v1/workflows/${workflowId}`, null, { headers });
    check(deleteRes, {
      'workflow deleted': (r) => r.status === 200 || r.status === 204,
    });
  });

  group('Health & Metrics', () => {
    const healthRes = http.get(`${BASE_URL}/api/v1/health`);
    check(healthRes, {
      'health check ok': (r) => r.status === 200,
    });

    const metricsRes = http.get(`${BASE_URL}/api/v1/metrics`);
    check(metricsRes, {
      'metrics available': (r) => r.status === 200,
    });
  });

  sleep(1);
}

export function handleSummary(data) {
  return {
    'stdout': textSummary(data, { indent: '  ', enableColors: true }),
    'tests/load/results.json': JSON.stringify(data),
  };
}

function textSummary(data, opts) {
  const metrics = data.metrics;
  let summary = '\n=== FlowForge Load Test Results ===\n\n';

  summary += `HTTP Requests: ${metrics.http_reqs?.values?.count || 0}\n`;
  summary += `Request Duration (p95): ${metrics.http_req_duration?.values?.['p(95)']?.toFixed(2) || 'N/A'}ms\n`;
  summary += `Request Duration (p99): ${metrics.http_req_duration?.values?.['p(99)']?.toFixed(2) || 'N/A'}ms\n`;
  summary += `Failed Requests: ${(metrics.http_req_failed?.values?.rate * 100)?.toFixed(2) || 0}%\n`;
  summary += `Workflow Create Success: ${(metrics.workflow_create_success?.values?.rate * 100)?.toFixed(2) || 0}%\n`;
  summary += `Workflow Trigger Success: ${(metrics.workflow_trigger_success?.values?.rate * 100)?.toFixed(2) || 0}%\n`;

  return summary;
}
