import{i as e,o as t,s as n}from"./workbench-BFpUAeSb.js";var r=`/graphql`;function i(){return typeof window>`u`?``:window.localStorage.getItem(`anycode.accessKey`)??window.localStorage.getItem(`ANYCODE_ACCESS_KEY`)??``}async function a({query:e,variables:t,operationName:n}){let a=new Headers({"content-type":`application/json`}),o=i();o&&a.set(`authorization`,`Bearer ${o}`);let s=await fetch(r,{method:`POST`,headers:a,body:JSON.stringify({query:e,variables:t,operationName:n})});if(!s.ok)throw Error(`GraphQL request failed: ${s.status}`);let c=await s.json();if(c.errors?.length)throw Error(c.errors.map(e=>e.message).join(`; `));if(!c.data)throw Error(`GraphQL response missing data`);return c.data}var o=`
  id
  projectId
  projectName
  requirement
  requirementSummary
  mode
  status
  baseBranch
  currentNodeTitle
  pendingQuestion
  lastRunAt
  createdAt
  updatedAt
`,s=`
  id
  projectId
  requirement
  mode
  status
  baseBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  lastRunAt
  createdAt
  updatedAt
`;async function c(e={}){try{let t=await a({query:`
        query Sessions($input: ListSessionsInput) {
          sessions(input: $input) {
            items {
              ${o}
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      `,variables:{input:e}});return{items:t.sessions.items.map(f),pageInfo:t.sessions.pageInfo}}catch{return h(e)}}async function l(n){try{let e=await a({query:`
          query Session($id: ID!) {
            session(id: $id) {
              ${s}
            }
          }
        `,variables:{id:n}}),r=[...t];try{r=(await a({query:`
          query SessionEvents($input: ListSessionEventsInput!) {
            sessionEvents(input: $input) {
              items {
                id
                type
                payload
                createdAt
              }
              pageInfo {
                page
                pageSize
                total
                nextCursor
              }
            }
          }
        `,variables:{input:{sessionId:n,page:1,pageSize:50}}})).sessionEvents.items.map(m)}catch{r=[...t]}return{session:p(e.session),events:r}}catch{return{session:e(n),events:[...t]}}}async function u(e,t){try{return await a({query:`
        mutation AppendPrompt($input: AppendPromptInput!) {
          appendPrompt(input: $input) {
            id
            sessionId
            body
            createdAt
          }
        }
      `,variables:{input:{sessionId:e,body:t}}})}catch{return{appendPrompt:{id:`local-${Date.now()}`,sessionId:e,body:t,createdAt:new Date().toISOString()}}}}async function d(e){try{return p((await a({query:`
        mutation CreateSession($input: CreateSessionInput!) {
          createSession(input: $input) {
            ${s}
          }
        }
      `,variables:{input:e}})).createSession)}catch{return p({id:`local-${Date.now()}`,projectId:e.projectId,requirement:e.requirement,mode:e.mode,status:`stopped`,baseBranch:e.baseBranch??`main`,config:{codexModel:e.config?.codexModel??``,reasoningEffort:e.config?.reasoningEffort??``,permissionMode:e.config?.permissionMode??``},lastRunAt:null,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()})}}function f(e){return{id:e.id,projectId:e.projectId,title:e.requirementSummary||S(e.requirement),summary:e.requirementSummary||e.requirement,mode:g(e.mode),status:_(e.status),branch:e.baseBranch||`main`,node:e.currentNodeTitle||b(_(e.status)),updatedAt:C(e.lastRunAt??e.updatedAt),pendingQuestion:e.pendingQuestion,filesChanged:0}}function p(e){let t=_(e.status);return{id:e.id,projectId:e.projectId,title:S(e.requirement),summary:e.requirement,mode:g(e.mode),status:t,branch:e.baseBranch||`main`,node:b(t),updatedAt:C(e.lastRunAt??e.updatedAt),pendingQuestion:t===`waiting_user`,filesChanged:0}}function m(e){return{id:e.id,kind:v(e.type),title:x(e.payload,`title`)||y(e.type),body:x(e.payload,`body`)||x(e.payload,`message`)||JSON.stringify(e.payload),time:w(e.createdAt)}}function h(e){let t=e.page??1,r=e.pageSize??n.length,i=(t-1)*r;return{items:n.slice(i,i+r),pageInfo:{page:t,pageSize:r,total:n.length,nextCursor:i+r<n.length?String(t+1):``}}}function g(e){return e===`chat`?`chat`:`workflow`}function _(e){return e===`running`||e===`waiting_user`||e===`stopped`||e===`blocked`||e===`completed`?e:`stopped`}function v(e){return e.includes(`tool`)?`tool`:e.includes(`assistant`)?`assistant`:e.includes(`question`)?`question`:e.includes(`thought`)?`thought`:`status`}function y(e){return e.includes(`tool`)?`工具调用`:e.includes(`assistant`)?`模型输出`:e.includes(`question`)?`待回答`:e.includes(`thought`)?`思考`:`状态`}function b(e){return{running:`运行中`,waiting_user:`待回答`,stopped:`已停止`,blocked:`阻塞`,completed:`已完成`}[e]}function x(e,t){let n=e[t];return typeof n==`string`?n:``}function S(e){return e.split(`
`).find(e=>e.trim())?.trim()||`未命名会话`}function C(e){return e?new Intl.DateTimeFormat(`zh-CN`,{month:`2-digit`,day:`2-digit`,hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}function w(e){return e?new Intl.DateTimeFormat(`zh-CN`,{hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}export{a,c as i,d as n,l as r,u as t};