import { test, describe } from 'node:test';
import * as assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const SCENARIOS_PATH = resolve(__dirname, '../../src/data/scenarios.json');
const GATE_TEST_PATH = resolve(__dirname, '../../../agentgate/qa/v1security/security_gate_test.go');

describe('scenarios.json', () => {
  let scenarios;
  let gateTestSource;

  test('loads data and source', () => {
    const rawData = readFileSync(SCENARIOS_PATH, 'utf-8');
    scenarios = JSON.parse(rawData);
    gateTestSource = readFileSync(GATE_TEST_PATH, 'utf-8');
  });

  test('has exactly 8 scenarios', () => {
    assert.equal(scenarios.length, 8);
  });

  test('ids are unique', () => {
    const ids = new Set();
    for (const sc of scenarios) {
      assert.ok(!ids.has(sc.id), `Duplicate id: ${sc.id}`);
      ids.add(sc.id);
    }
  });

  test('schema is valid', () => {
    for (const sc of scenarios) {
      assert.ok(typeof sc.id === 'string');
      assert.ok(typeof sc.title === 'string');
      assert.ok(typeof sc.attack === 'string');
      assert.ok(Array.isArray(sc.steps));
      assert.ok(typeof sc.result === 'string');
      assert.ok(['blocked', 'allowed'].includes(sc.outcome));
      assert.ok(typeof sc.source_test === 'string');
      // http_status can be number, string, or null
    }
  });

  test('no text contains "open source"', () => {
    const rawData = readFileSync(SCENARIOS_PATH, 'utf-8');
    assert.ok(!rawData.toLowerCase().includes('open source'), 'Found "open source" in JSON');
  });

  test('source_test matches func Test... in Go source', () => {
    for (const sc of scenarios) {
      const regex = new RegExp(`func ${sc.source_test}\\(t \\*testing\\.T\\)`);
      assert.ok(regex.test(gateTestSource), `Source test ${sc.source_test} not found in Go file`);
    }
  });

  test('http_status matches Go test assertions', () => {
    for (const sc of scenarios) {
      // Find the function body in the go source
      const funcStartIdx = gateTestSource.indexOf(`func ${sc.source_test}(t *testing.T)`);
      assert.ok(funcStartIdx !== -1, `Test ${sc.source_test} not found`);

      // Simplistic parsing: extract from funcStartIdx to the next \n}
      let funcEndIdx = gateTestSource.indexOf('\n}', funcStartIdx);
      if (funcEndIdx === -1) funcEndIdx = gateTestSource.length;
      
      const funcBody = gateTestSource.substring(funcStartIdx, funcEndIdx);

      // Check for literals: StatusUnauthorized -> 401, StatusForbidden -> 403, StatusOK -> 200
      let expectedStatus = null;
      if (funcBody.includes('!= http.StatusUnauthorized') || (funcBody.includes('StatusUnauthorized') && !funcBody.includes('StatusOK'))) {
        expectedStatus = 401;
      } else if (funcBody.includes('!= http.StatusForbidden') || (funcBody.includes('StatusForbidden') && !funcBody.includes('StatusOK'))) {
        expectedStatus = 403;
      } else if (sc.http_status === 'non-200' || sc.http_status === null) {
         assert.ok(!funcBody.includes('StatusUnauthorized'), `Test ${sc.source_test} has StatusUnauthorized but JSON says ${sc.http_status}`);
         assert.ok(!funcBody.includes('StatusForbidden'), `Test ${sc.source_test} has StatusForbidden but JSON says ${sc.http_status}`);
         expectedStatus = sc.http_status;
      }

      assert.equal(sc.http_status, expectedStatus, `Mismatch for ${sc.id}: expected ${expectedStatus}, got ${sc.http_status}`);
    }
  });
});
