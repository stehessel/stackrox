import React, { useEffect, useState } from 'react';
import { withRouter } from 'react-router-dom';
import PageHeader from 'Components/PageHeader';
import EntityTabs from 'Components/workflow/EntityTabs';
import EntitiesMenu from 'Components/workflow/EntitiesMenu';
import workflowStateContext from 'Containers/workflowStateContext';
import parseURL from 'modules/URLParser';
import getSidePanelEntity from 'utils/getSidePanelEntity';
import { searchParams, sortParams, pagingParams } from 'constants/searchParams';
import { WorkflowState } from 'modules/WorkflowState';
import { useCaseEntityMap } from 'modules/entityRelationships';
import entityLabels from 'messages/entity';
import useEntityName from 'hooks/useEntityName';
import WorkflowSidePanel from './WorkflowSidePanel';
import { EntityComponentMap } from './UseCaseComponentMaps';

const WorkflowEntityPageLayout = ({ location }) => {
    const workflowState = parseURL(location);
    const { stateStack, useCase, search } = workflowState;
    const pageState = new WorkflowState(useCase, workflowState.getPageStack(), search);

    // Entity Component
    const EntityComponent = EntityComponentMap[useCase];

    // Page props
    const pageEntity = workflowState.getBaseEntity();
    const { entityId: pageEntityId, entityType: pageEntityType } = pageEntity;
    const pageListType = stateStack[1] && stateStack[1].entityType;
    const pageSearch = workflowState.search[searchParams.page];
    const pageSort = workflowState.sort[sortParams.page];
    const pagePaging = workflowState.paging[pagingParams.page];

    // Sidepanel props
    const { sidePanelEntityId, sidePanelEntityType, sidePanelListType } = getSidePanelEntity(
        workflowState
    );
    const sidePanelSearch = workflowState.search[searchParams.sidePanel];
    const sidePanelSort = workflowState.sort[sortParams.sidePanel];
    const sidePanelPaging = workflowState.paging[pagingParams.sidePanel];

    const [fadeIn, setFadeIn] = useState(false);
    useEffect(() => setFadeIn(false), []);

    // manually adding the styles to fade back in
    if (!fadeIn) setTimeout(() => setFadeIn(true), 50);
    const style = fadeIn
        ? {
              opacity: 1,
              transition: '.15s opacity ease-in',
              transitionDelay: '.25s'
          }
        : {
              opacity: 0
          };

    const subheaderText = entityLabels[pageEntityType];
    const { entityName = '' } = useEntityName(pageEntityType, pageEntityId);

    return (
        <workflowStateContext.Provider value={pageState}>
            <div className="flex flex-1 flex-col bg-base-200" style={style}>
                <PageHeader header={entityName} subHeader={subheaderText} classes="pr-0">
                    <div className="flex flex-1 justify-end h-full">
                        <EntitiesMenu
                            text="All Entities"
                            options={useCaseEntityMap[useCase]}
                            grouped
                        />
                    </div>
                </PageHeader>
                <EntityTabs entityType={pageEntityType} activeTab={pageListType} />
                <div className="flex flex-1 w-full h-full bg-base-100 relative z-0 overflow-hidden">
                    <div
                        className={`${
                            sidePanelEntityId ? 'overlay' : ''
                        } h-full w-full overflow-auto`}
                        id="capture-list"
                    >
                        <EntityComponent
                            entityType={pageEntityType}
                            entityId={pageEntityId}
                            entityListType={pageListType}
                            search={pageSearch}
                            sort={pageSort}
                            page={pagePaging}
                        />
                    </div>

                    <WorkflowSidePanel isOpen={!!sidePanelEntityId}>
                        {sidePanelEntityId ? (
                            <EntityComponent
                                entityId={sidePanelEntityId}
                                entityType={sidePanelEntityType}
                                entityListType={sidePanelListType}
                                search={sidePanelSearch}
                                sort={sidePanelSort}
                                page={sidePanelPaging}
                                entityContext={{
                                    [pageEntity.entityType]: pageEntity.entityId
                                }}
                            />
                        ) : (
                            <span />
                        )}
                    </WorkflowSidePanel>
                </div>
            </div>
        </workflowStateContext.Provider>
    );
};

export default withRouter(WorkflowEntityPageLayout);
