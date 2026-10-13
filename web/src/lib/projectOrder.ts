import type { BoardsRepoInfo, ProjectConfig } from '../types';
import { boardsRepoName } from '../types';

/**
 * Sidebar order: boards-repo group (config order, only when several repos are
 * configured), then name. The sidebar, the 1-9 shortcuts and every project
 * dropdown share it so they cannot disagree.
 */
export function sortProjectsForSidebar(
  projects: ProjectConfig[],
  boardsRepos: BoardsRepoInfo[],
): ProjectConfig[] {
  const rank = (p: ProjectConfig): number => {
    if (boardsRepos.length <= 1) return 0;
    const name = boardsRepoName(p.boards_repo, boardsRepos);
    return boardsRepos.findIndex((r) => r.name === name);
  };
  return [...projects].sort(
    (a, b) =>
      rank(a) - rank(b) ||
      a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }),
  );
}
