import { describe, it, expect, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useCreateCardForm } from './useCreateCardForm';
import type { CreateCardForm } from './useCreateCardForm';
import type { ProjectConfig } from '../../types';

const config: ProjectConfig = {
  name: 'test',
  prefix: 'TEST',
  next_id: 1,
  states: ['todo', 'in_progress', 'done'],
  types: ['task'],
  priorities: ['low', 'medium', 'high'],
  transitions: { todo: ['in_progress'], in_progress: ['done'], done: [] },
};

describe('useCreateCardForm - isDirty', () => {
  it('is false for a pristine form', () => {
    const { result } = renderHook(() => useCreateCardForm(config, vi.fn()));
    expect(result.current.form.isDirty).toBe(false);
  });

  it.each<[string, (f: CreateCardForm) => void]>([
    ['assignee', (f) => f.setAssignee('alice')],
    ['orchestrator model pin', (f) => f.setModelOrchestrator('vendor/model')],
    ['coder model pin', (f) => f.setModelCoder('vendor/model')],
    ['reviewer model pin', (f) => f.setModelReviewer('vendor/model')],
    ['skills', (f) => f.setSkills(['go-development'])],
    ['mob guests', (f) => f.setMobGuests(['guest'])],
  ])('is true once %s is set', (_name, change) => {
    const { result } = renderHook(() => useCreateCardForm(config, vi.fn()));
    act(() => change(result.current.form));
    expect(result.current.form.isDirty).toBe(true);
  });

  it('ignores an automation toggle changed from its seed', () => {
    const { result } = renderHook(() => useCreateCardForm(config, vi.fn()));
    act(() => result.current.form.setAutonomous(!result.current.form.autonomous));
    expect(result.current.form.isDirty).toBe(false);
  });
});
