<template>
  <router-view
    :key="$route.name === 'shared-session' ? String($route.params.shareId) : 'application'"
  />
</template>
