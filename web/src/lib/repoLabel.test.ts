import { describe, it, expect } from 'vitest';
import { repoLabel } from './repoLabel';

describe('repoLabel', () => {
  it.each([
    ['git@github.com:example/homelander.git', 'example/homelander'],
    ['https://github.com/example/homelander', 'example/homelander'],
    ['https://ghe.example.com/team/svc.git/', 'team/svc'],
    ['ssh://git@gitea.local:2222/ops/infra.git', 'ops/infra'],
    ['plain-name', 'plain-name'],
    ['', ''],
    [undefined, ''],
  ])('shortens %s', (input, want) => {
    expect(repoLabel(input)).toBe(want);
  });
});
