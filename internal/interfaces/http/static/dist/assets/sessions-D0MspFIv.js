import{a as e}from"./workbench-BvTTHgXe.js";var t=`anycode.accessKey`,n=`/graphql`;function r(){return typeof window>`u`?``:window.localStorage.getItem(`anycode.accessKey`)??window.localStorage.getItem(`ANYCODE_ACCESS_KEY`)??``}function i(e){if(!(typeof window>`u`)){if(e.trim()===``){window.localStorage.removeItem(t);return}window.localStorage.setItem(t,e.trim())}}async function a({query:e,variables:t,operationName:i}){let a=new Headers({"content-type":`application/json`}),o=r();o&&a.set(`authorization`,`Bearer ${o}`);let s=await fetch(n,{method:`POST`,headers:a,body:JSON.stringify({query:e,variables:t,operationName:i})});if(!s.ok)throw Error(`GraphQL request failed: ${s.status}`);let c=await s.json();if(c.errors?.length)throw Error(c.errors.map(e=>e.message).join(`; `));if(!c.data)throw Error(`GraphQL response missing data`);return c.data}var o=`
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
  availableActions
  canResume
  lastRunAt
  createdAt
  updatedAt
`,c=`
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
`;async function l(e={}){try{let t=await a({query:`
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
      `,variables:{input:e}});return{items:t.sessions.items.map(m),pageInfo:t.sessions.pageInfo}}catch{return v(e)}}async function u(e){let[t,n]=await Promise.all([a({query:`
        query Session($id: ID!) {
          session(id: $id) {
            ${s}
          }
        }
      `,variables:{id:e}}),a({query:`
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
      `,variables:{input:{sessionId:e,page:1,pageSize:50}}})]);return{session:h(t.session),events:n.sessionEvents.items.map(_)}}async function d(e,t){return a({query:`
      mutation AppendPrompt($input: AppendPromptInput!) {
        appendPrompt(input: $input) {
          id
          sessionId
          body
          createdAt
        }
      }
    `,variables:{input:{sessionId:e,body:t}}})}async function f(e){return a({query:`
      mutation StopSession($id: ID!) {
        stopSession(id: $id) {
          ${c}
        }
      }
    `,variables:{id:e}})}async function p(e){try{return g((await a({query:`
        mutation CreateSession($input: CreateSessionInput!) {
          createSession(input: $input) {
            ${c}
          }
        }
      `,variables:{input:e}})).createSession)}catch{return g({id:`local-${Date.now()}`,projectId:e.projectId,requirement:e.requirement,mode:e.mode,status:`stopped`,baseBranch:e.baseBranch??`main`,config:{codexModel:e.config?.codexModel??``,reasoningEffort:e.config?.reasoningEffort??``,permissionMode:e.config?.permissionMode??``},lastRunAt:null,createdAt:new Date().toISOString(),updatedAt:new Date().toISOString()})}}function m(e){return{id:e.id,projectId:e.projectId,title:e.requirementSummary||T(e.requirement),summary:e.requirementSummary||e.requirement,mode:y(e.mode),status:b(e.status),branch:e.baseBranch||`main`,node:e.currentNodeTitle||C(b(e.status)),updatedAt:E(e.lastRunAt??e.updatedAt),pendingQuestion:e.pendingQuestion,filesChanged:0}}function h(e){let t=b(e.status);return{id:e.id,projectId:e.projectId,title:T(e.requirement),summary:e.requirement,mode:y(e.mode),status:t,branch:e.baseBranch||`main`,node:C(t),updatedAt:E(e.lastRunAt??e.updatedAt),pendingQuestion:t===`waiting_user`,filesChanged:0,config:e.config,availableActions:e.availableActions,canResume:e.canResume}}function g(e){let t=b(e.status);return{id:e.id,projectId:e.projectId,title:T(e.requirement),summary:e.requirement,mode:y(e.mode),status:t,branch:e.baseBranch||`main`,node:C(t),updatedAt:E(e.lastRunAt??e.updatedAt),pendingQuestion:t===`waiting_user`,filesChanged:0}}function _(e){return{id:e.id,kind:x(e.type),title:w(e.payload,`title`)||S(e.type),body:w(e.payload,`body`)||w(e.payload,`message`)||JSON.stringify(e.payload),time:D(e.createdAt)}}function v(t){let n=t.page??1,r=t.pageSize??e.length,i=(n-1)*r;return{items:e.slice(i,i+r),pageInfo:{page:n,pageSize:r,total:e.length,nextCursor:i+r<e.length?String(n+1):``}}}function y(e){return e===`chat`?`chat`:`workflow`}function b(e){return new Set([`created`,`starting`,`running`,`waiting_user`,`stopping`,`stopped`,`resume_failed`,`failed`,`blocked`,`completed`,`closed`]).has(e)?e:`stopped`}function x(e){return e.includes(`tool`)?`tool`:e.includes(`assistant`)?`assistant`:e.includes(`question`)?`question`:e.includes(`thought`)?`thought`:`status`}function S(e){return e.includes(`tool`)?`工具调用`:e.includes(`assistant`)?`模型输出`:e.includes(`question`)?`待回答`:e.includes(`thought`)?`思考`:`状态`}function C(e){return{created:`待运行`,starting:`启动中`,running:`运行中`,waiting_user:`待回答`,stopping:`停止中`,stopped:`已停止`,resume_failed:`恢复失败`,failed:`失败`,blocked:`阻塞`,completed:`已完成`,closed:`已关闭`}[e]}function w(e,t){let n=e[t];return typeof n==`string`?n:``}function T(e){return e.split(`
`).find(e=>e.trim())?.trim()||`未命名会话`}function E(e){return e?new Intl.DateTimeFormat(`zh-CN`,{month:`2-digit`,day:`2-digit`,hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}function D(e){return e?new Intl.DateTimeFormat(`zh-CN`,{hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}export{f as a,i as c,l as i,p as n,r as o,u as r,a as s,d as t};