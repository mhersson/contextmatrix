import { useState } from 'react';
import { useNavigate } from 'react-router';
import { api } from '../../api/client';
import { useTheme } from '../../hooks/useTheme';
import { useToast } from '../../hooks/useToast';
import type { CreatePlaybookInput } from '../../types';

export function usePlaybookCreateForm() {
  const [creating, setCreating] = useState(false);
  const [newTitle, setNewTitle] = useState('');
  const [newDescription, setNewDescription] = useState('');
  const [newRepo, setNewRepo] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const { boardsRepos = [] } = useTheme();
  const multiRepo = boardsRepos.length > 1;
  const { showToast } = useToast();
  const navigate = useNavigate();

  const handleCreate = async () => {
    if (submitting) return;
    const title = newTitle.trim();
    if (!title) return;
    setSubmitting(true);
    try {
      const input: CreatePlaybookInput = { title };
      const description = newDescription.trim();
      if (description) input.description = description;
      if (multiRepo) input.boards_repo = newRepo || boardsRepos[0].name;
      const detail = await api.createPlaybook(input);
      navigate(`/playbooks/${detail.id}`);
    } catch {
      showToast('Failed to create playbook', 'error');
    } finally {
      setSubmitting(false);
    }
  };

  const cancelCreate = () => {
    setCreating(false);
    setNewTitle('');
    setNewDescription('');
    setNewRepo('');
  };

  return {
    creating, setCreating, newTitle, setNewTitle, newDescription, setNewDescription,
    newRepo, setNewRepo, submitting, boardsRepos, multiRepo, handleCreate, cancelCreate,
  };
}
