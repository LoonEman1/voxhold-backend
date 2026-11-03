# Дизайн REF-01: распил WorkspacePage.tsx

Статус: черновик на утверждение.

## Правила

Один PR — вынос одного хука или группы связанного состояния; код переносится **без изменения
логики**; все тесты и Playwright UI зелёные; бюджет `WorkspacePage` ≤ 300000 байт соблюдён;
фичи с рефакторингом не смешиваются.

## Порядок (по независимости)

1. `useWorkspaceDialogs` (состояние диалогов `dialog`, `editingChannel` и т. п.).
2. `useRealtimeEvents` (создание `RealtimeClient` и обработчики событий).
3. `useVoiceSession`.
4. `useStreamSession`.
5. Остаток разбивается на компоненты `ChannelSidebar`, `ServerHeader`, `Dialogs`.

## Риски

Сотни хуков с перекрёстными зависимостями и замыканиями на `token`, `selectedChannelId`;
перенос меняет порядок вызова хуков — проверять eslint `react-hooks`. Каждый PR сопровождать
замером размера чанка (`scripts/check-bundle.mjs`).
