import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { JiraImportResults } from './JiraImportResults';
import type { JiraImportResult } from '../../types';

const row = (key: string, status: JiraImportResult['status']): JiraImportResult => ({ key, status });

describe('JiraImportResults', () => {
  it.each([
    ['an all-failed import', [row('A-1', 'failed'), row('A-2', 'failed'), row('A-3', 'failed')], /\b0 of 3\b/],
    ['a mixed import', [row('A-1', 'created'), row('A-2', 'skipped'), row('A-3', 'failed')], /\b1 of 3\b/],
    ['a clean import', [row('A-1', 'created'), row('A-2', 'created'), row('A-3', 'created')], /\b3 of 3\b/],
  ])('counts only created issues as imported for %s', (_, results, count) => {
    render(
      <JiraImportResults
        run={{ status: 'done', results, total: 3, playbookId: 'p' }}
        keys={['A-1', 'A-2', 'A-3']}
        onOpen={() => {}}
        onClose={() => {}}
      />,
    );
    expect(screen.getByRole('status')).toHaveTextContent(count);
  });
});
