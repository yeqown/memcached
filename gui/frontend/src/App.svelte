<script lang="ts">
  import Sidebar from './components/Sidebar.svelte'
  import OperationPanel from './components/OperationPanel.svelte'
  import ProtocolLog from './components/ProtocolLog.svelte'
  import ValueDisplay from './components/ValueDisplay.svelte'
  import ConnectionBanner from './components/ConnectionBanner.svelte'
  import { activeOperationTab } from './stores/app'
</script>

<div class="app-layout">
  <div class="main-content">
    <div class="left-column">
      <Sidebar />
    </div>
    <div class="right-column">
      <div class="right-top">
        <ConnectionBanner />
      </div>
      <OperationPanel />
      {#if $activeOperationTab !== 'set' && $activeOperationTab !== 'delete'}
        <div class="result-area">
          <ValueDisplay />
        </div>
      {/if}
    </div>
  </div>

  <div class="log-dock-full">
    <ProtocolLog />
  </div>
</div>

<style>
  .app-layout {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
    background: var(--bg-primary);
    color: var(--text-primary);
  }

  .main-content {
    flex: 1;
    display: flex;
    min-height: 0;
    overflow: hidden;
  }

  .left-column {
    display: flex;
    flex-direction: column;
    width: 260px;
    min-width: 260px;
    border-right: 1px solid var(--border);
    overflow: hidden;
  }

  .right-column {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    min-width: 0;
  }

  .right-top {
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    background: var(--bg-surface);
  }

  .right-top :global(.banner) {
    width: 100%;
    border-bottom: none;
  }

  .result-area {
    flex: 1;
    overflow: hidden;
    min-height: 0;
  }

  .log-dock-full {
    flex: 0 0 30%;
    min-height: 192px;
    max-height: 440px;
    border-top: 1px solid var(--border);
    background: var(--bg-surface);
    overflow: hidden;
  }
</style>
